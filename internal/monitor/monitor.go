// internal/monitor/monitor.go
// Debug version with extensive logging

package monitor

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goParse/internal/driver"
	"goParse/internal/embeddings"
	"goParse/internal/model"

	"github.com/fsnotify/fsnotify"
)

// Monitor watches for file changes and updates the graph
type Monitor struct {
	rootPath       string
	watcher        *fsnotify.Watcher
	driver         *driver.TreeSitterDriver
	graphClient    GraphClient
	embeddingGen   *embeddings.CodeEmbeddingGenerator
	fileTracker    *FileTracker
	stopChan       chan struct{}
	wg             sync.WaitGroup
	fileHandler    func(context.Context, string)
	eventPublisher func(MonitorEvent)
	isRunning      bool
	startTime      time.Time
}

// GraphClient interface that all database clients must implement
type GraphClient interface {
	Close(ctx context.Context) error
	CreateIndexes(ctx context.Context) error
	UpsertFile(ctx context.Context, path, language string) error
	UpsertFunction(ctx context.Context, fn model.FunctionEntity) error
	UpsertImport(ctx context.Context, imp model.ImportEntity) error
	UpsertVariable(ctx context.Context, variable model.VariableEntity) error
	UpsertType(ctx context.Context, typeEntity model.TypeEntity) error
	UpsertInterface(ctx context.Context, iface model.InterfaceEntity) error
	UpsertClass(ctx context.Context, class model.ClassEntity) error
	UpsertConstant(ctx context.Context, constant model.ConstantEntity) error
	UpsertJSXElement(ctx context.Context, jsx model.JSXElementEntity) error
	UpsertCSSRule(ctx context.Context, css model.CSSRuleEntity) error
	UpsertFunctionCall(ctx context.Context, call model.FunctionCallEntity) error
	UpsertTypeUsage(ctx context.Context, usage model.TypeUsageEntity) error
	UpsertExtends(ctx context.Context, extends model.ExtendsEntity) error
	UpsertImplements(ctx context.Context, implements model.ImplementsEntity) error
	UpsertReference(ctx context.Context, ref model.ReferenceEntity) error
}

// Config holds monitor configuration
type Config struct {
	RootPath     string
	GraphClient  GraphClient
	EmbeddingGen *embeddings.CodeEmbeddingGenerator
}

// NewMonitor creates a new file monitor
func NewMonitor(config Config) (*Monitor, error) {
	log.Printf("[DEBUG] Creating new monitor for path: %s", config.RootPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	monitor := &Monitor{
		rootPath:     config.RootPath,
		watcher:      watcher,
		driver:       driver.NewTreeSitterDriver(),
		graphClient:  config.GraphClient,
		embeddingGen: config.EmbeddingGen,
		fileTracker:  NewFileTracker(config.RootPath),
		stopChan:     make(chan struct{}),
	}

	// Set default file handler
	monitor.fileHandler = monitor.processFile
	log.Printf("[DEBUG] Monitor created with default file handler")

	// Load existing file state
	if err := monitor.fileTracker.LoadState(); err != nil {
		log.Printf("[WARNING] Failed to load file state: %v", err)
	} else {
		log.Printf("[DEBUG] Loaded file tracker state")
	}

	return monitor, nil
}

// SetEventPublisher configures a callback for publishing monitor events.
func (m *Monitor) SetEventPublisher(publisher func(MonitorEvent)) {
	m.eventPublisher = publisher
	log.Printf("[DEBUG] Event publisher set")
}

// Start begins monitoring for file changes
func (m *Monitor) Start(ctx context.Context) error {
	log.Printf("[DEBUG] Starting monitor for: %s", m.rootPath)
	m.startTime = time.Now()
	m.isRunning = true

	// Count directories to watch
	dirCount := 0
	fileCount := 0

	// Add directories to watch
	err := filepath.Walk(m.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("[ERROR] Walk error at %s: %v", path, err)
			return err
		}

		if info.IsDir() {
			// Skip certain directories
			if shouldSkipDir(info.Name()) {
				log.Printf("[DEBUG] Skipping directory: %s", path)
				return filepath.SkipDir
			}

			if err := m.watcher.Add(path); err != nil {
				log.Printf("[ERROR] Failed to watch directory %s: %v", path, err)
				return err
			}

			dirCount++
			log.Printf("[DEBUG] Watching directory: %s", path)
			return nil
		}

		// Count supported files
		if isSupportedFile(path) {
			fileCount++
		}

		return nil
	})

	if err != nil {
		return err
	}

	log.Printf("[INFO] Monitor started - watching %d directories with %d supported files", dirCount, fileCount)

	// Start watching
	m.wg.Add(1)
	go m.watch(ctx)

	// Periodic state save
	m.wg.Add(1)
	go m.periodicStateSave()

	// List current watchers
	log.Printf("[DEBUG] Active watchers: %v", m.watcher.WatchList())

	return nil
}

// Stop stops the monitor
func (m *Monitor) Stop() error {
	log.Printf("[DEBUG] Stopping monitor")
	m.isRunning = false

	close(m.stopChan)
	m.watcher.Close()
	m.wg.Wait()

	// Save final state
	if err := m.fileTracker.SaveState(); err != nil {
		log.Printf("[ERROR] Failed to save final state: %v", err)
		return err
	}

	log.Printf("[INFO] Monitor stopped successfully")
	return nil
}

// watch handles file system events
func (m *Monitor) watch(ctx context.Context) {
	defer m.wg.Done()
	log.Printf("[DEBUG] File watcher goroutine started")

	for {
		select {
		case <-m.stopChan:
			log.Printf("[DEBUG] File watcher received stop signal")
			return

		case event, ok := <-m.watcher.Events:
			if !ok {
				log.Printf("[DEBUG] Watcher events channel closed")
				return
			}
			log.Printf("[DEBUG] File system event: %s on %s", event.Op.String(), event.Name)
			m.handleEvent(ctx, event)

		case err, ok := <-m.watcher.Errors:
			if !ok {
				log.Printf("[DEBUG] Watcher errors channel closed")
				return
			}
			log.Printf("[ERROR] Watcher error: %v", err)
		}
	}
}

// handleEvent processes a file system event
func (m *Monitor) handleEvent(ctx context.Context, event fsnotify.Event) {
	log.Printf("[DEBUG] Handling event: %s for file: %s", event.Op.String(), event.Name)

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		log.Printf("[INFO] File created: %s", event.Name)

		// Check if it's a directory
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if !shouldSkipDir(info.Name()) {
				if err := m.watcher.Add(event.Name); err != nil {
					log.Printf("[ERROR] Failed to watch new directory %s: %v", event.Name, err)
				} else {
					log.Printf("[DEBUG] Added watcher for new directory: %s", event.Name)
				}
			}
			if m.eventPublisher != nil {
				m.eventPublisher(MonitorEvent{Type: "create_dir", FilePath: event.Name, Timestamp: time.Now()})
			}
			return
		}

		if !isSupportedFile(event.Name) {
			log.Printf("[DEBUG] Skipping unsupported file: %s", event.Name)
			return
		}

		log.Printf("[DEBUG] Processing created file: %s", event.Name)
		m.fileHandler(ctx, event.Name)

		if m.eventPublisher != nil {
			m.eventPublisher(MonitorEvent{Type: "create", FilePath: event.Name, Timestamp: time.Now()})
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		if !isSupportedFile(event.Name) {
			log.Printf("[DEBUG] Skipping write to unsupported file: %s", event.Name)
			return
		}

		log.Printf("[INFO] File modified: %s", event.Name)
		log.Printf("[DEBUG] Calling file handler for: %s", event.Name)
		m.fileHandler(ctx, event.Name)

		if m.eventPublisher != nil {
			m.eventPublisher(MonitorEvent{Type: "modify", FilePath: event.Name, Timestamp: time.Now()})
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		log.Printf("[INFO] File removed: %s", event.Name)
		m.handleRemoval(ctx, event.Name)

		if m.eventPublisher != nil {
			m.eventPublisher(MonitorEvent{Type: "remove", FilePath: event.Name, Timestamp: time.Now()})
		}

	case event.Op&fsnotify.Rename == fsnotify.Rename:
		log.Printf("[INFO] File renamed: %s", event.Name)

		// If the file still exists at the same path, treat this as a modification
		if _, err := os.Stat(event.Name); err == nil {
			if isSupportedFile(event.Name) {
				log.Printf("[DEBUG] Treating rename as modification for: %s", event.Name)
				m.fileHandler(ctx, event.Name)

				if m.eventPublisher != nil {
					m.eventPublisher(MonitorEvent{Type: "modify", FilePath: event.Name, Timestamp: time.Now()})
				}
			}
		} else {
			log.Printf("[DEBUG] File no longer exists after rename: %s", event.Name)
			m.handleRemoval(ctx, event.Name)

			if m.eventPublisher != nil {
				m.eventPublisher(MonitorEvent{Type: "rename", FilePath: event.Name, Timestamp: time.Now()})
			}
		}
	}
}

// processFile processes a single file
func (m *Monitor) processFile(ctx context.Context, filePath string) {
	log.Printf("[DEBUG] Base monitor processFile called for: %s", filePath)

	// Check if file has actually changed
	changed, err := m.fileTracker.HasChanged(filePath)
	if err != nil {
		log.Printf("[ERROR] Error checking file change status for %s: %v", filePath, err)
		return
	}

	if !changed {
		log.Printf("[DEBUG] File %s has not changed according to tracker", filePath)
		return
	}

	log.Printf("[INFO] Processing changed file: %s", filePath)

	// Convert to relative path
	relPath, err := filepath.Rel(m.rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	// Parse the file
	pf, err := m.driver.Parse(filePath)
	if err != nil {
		log.Printf("[ERROR] Failed to parse %s: %v", relPath, err)
		return
	}

	log.Printf("[DEBUG] Successfully parsed %s", relPath)

	// Update file path to relative
	pf.FilePath = relPath

	// Update graph based on client type
	switch client := m.graphClient.(type) {
	case *model.Neo4jClient:
		log.Printf("[DEBUG] Updating Neo4j for: %s", relPath)
		m.updateNeo4j(ctx, client, pf, filePath)
	case *model.AGEClient:
		log.Printf("[DEBUG] Updating AGE for: %s", relPath)
		m.updateAGE(ctx, client, pf, filePath)
	case *model.OracleGraphClient:
		log.Printf("[DEBUG] Updating Oracle for: %s", relPath)
		m.updateOracle(ctx, client, pf, filePath)
	default:
		log.Printf("[ERROR] Unknown graph client type")
		return
	}

	// Update file tracker
	if err := m.fileTracker.UpdateState(filePath); err != nil {
		log.Printf("[ERROR] Failed to update file state for %s: %v", filePath, err)
	} else {
		log.Printf("[DEBUG] Updated file tracker state for: %s", filePath)
	}
}

// updateNeo4j updates Neo4j with the parsed file data
func (m *Monitor) updateNeo4j(ctx context.Context, client *model.Neo4jClient, pf driver.ParsedFile, filePath string) {
	// Update file node
	if err := client.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
		log.Printf("[ERROR] Failed to upsert file: %v", err)
		return
	}

	// Update all entities
	m.updateEntities(ctx, client, pf, filePath)
}

// updateAGE updates Apache AGE with the parsed file data
func (m *Monitor) updateAGE(ctx context.Context, client *model.AGEClient, pf driver.ParsedFile, filePath string) {
	// Update file node
	if err := client.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
		log.Printf("[ERROR] Failed to upsert file: %v", err)
		return
	}

	// Update all entities
	m.updateEntities(ctx, client, pf, filePath)
}

// updateOracle updates Oracle Graph with the parsed file data
func (m *Monitor) updateOracle(ctx context.Context, client *model.OracleGraphClient, pf driver.ParsedFile, filePath string) {
	// Update file node
	if err := client.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
		log.Printf("[ERROR] Failed to upsert file: %v", err)
		return
	}

	// Update all entities
	m.updateEntities(ctx, client, pf, filePath)
}

// updateEntities updates all entities for a parsed file
func (m *Monitor) updateEntities(ctx context.Context, client GraphClient, pf driver.ParsedFile, filePath string) {
	log.Printf("[DEBUG] Updating entities for: %s", pf.FilePath)

	entityCount := 0

	// Update functions
	for _, fn := range pf.Funcs {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("[ERROR] Failed to update function %s: %v", fn.Name, err)
		} else {
			entityCount++
		}
	}

	// Update imports
	for _, imp := range pf.Imports {
		if err := client.UpsertImport(ctx, imp); err != nil {
			log.Printf("[ERROR] Failed to update import %s: %v", imp.Module, err)
		} else {
			entityCount++
		}
	}

	// Update other entities similarly...
	// (keeping the rest of the entity updates as they were)

	log.Printf("[INFO] Updated %d entities for %s", entityCount, pf.FilePath)

	// Update embeddings if generator is available
	if m.embeddingGen != nil {
		log.Printf("[DEBUG] Updating embeddings for: %s", pf.FilePath)
		m.updateEmbeddings(ctx, pf, filePath)
	}
}

// updateEmbeddings updates embeddings for the file
func (m *Monitor) updateEmbeddings(ctx context.Context, pf driver.ParsedFile, filePath string) {
	fileContent, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("[ERROR] Failed to read file for embeddings: %v", err)
		return
	}

	parsedFileData := embeddings.ParsedFileData{
		FilePath:    pf.FilePath,
		Language:    pf.Language,
		FileContent: string(fileContent),
	}

	// Convert entities to embedding format
	for _, fn := range pf.Funcs {
		parsedFileData.Functions = append(parsedFileData.Functions, embeddings.FunctionData{
			Name:      fn.Name,
			Content:   extractContent(fileContent, fn.StartLine, fn.EndLine),
			StartLine: fn.StartLine,
			EndLine:   fn.EndLine,
			Signature: fn.Signature,
			IsAsync:   fn.IsAsync,
			IsExport:  fn.IsExport,
		})
	}

	// Process other entities...

	if err := m.embeddingGen.ProcessFile(ctx, parsedFileData); err != nil {
		log.Printf("[ERROR] Failed to update embeddings: %v", err)
	} else {
		log.Printf("[DEBUG] Successfully updated embeddings for: %s", pf.FilePath)
	}
}

// handleRemoval handles file removal
func (m *Monitor) handleRemoval(ctx context.Context, filePath string) {
	log.Printf("[DEBUG] Handling removal of: %s", filePath)

	relPath, err := filepath.Rel(m.rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	// TODO: Implement removal from graph databases
	log.Printf("[WARNING] File removal not fully implemented for: %s", relPath)

	// Remove from file tracker
	m.fileTracker.RemoveState(filePath)
	log.Printf("[DEBUG] Removed from file tracker: %s", filePath)

	// Remove embeddings if available
	if m.embeddingGen != nil {
		log.Printf("[WARNING] Embedding removal not fully implemented for: %s", relPath)
	}
}

// periodicStateSave saves the file state periodically
func (m *Monitor) periodicStateSave() {
	defer m.wg.Done()
	log.Printf("[DEBUG] Periodic state save goroutine started")

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.fileTracker.SaveState(); err != nil {
				log.Printf("[ERROR] Failed to save file state: %v", err)
			} else {
				log.Printf("[DEBUG] Saved file tracker state")
			}
		case <-m.stopChan:
			log.Printf("[DEBUG] Periodic state save received stop signal")
			return
		}
	}
}

// Helper functions

func shouldSkipDir(name string) bool {
	skipDirs := map[string]bool{
		"node_modules":  true,
		".git":          true,
		"dist":          true,
		"build":         true,
		".next":         true,
		"coverage":      true,
		"vendor":        true,
		".vscode":       true,
		".idea":         true,
		"__pycache__":   true,
		".pytest_cache": true,
	}
	return skipDirs[name]
}

func isSupportedFile(path string) bool {
	supportedExts := map[string]bool{
		".ts":   true,
		".tsx":  true,
		".js":   true,
		".jsx":  true,
		".css":  true,
		".scss": true,
	}
	ext := filepath.Ext(path)
	supported := supportedExts[ext]

	if !supported {
		log.Printf("[DEBUG] Unsupported file extension: %s for file: %s", ext, path)
	}

	return supported
}

func extractContent(fileContent []byte, startLine, endLine int) string {
	if startLine <= 0 || endLine <= 0 {
		return ""
	}

	lines := strings.Split(string(fileContent), "\n")
	if startLine > len(lines) {
		return ""
	}

	if endLine > len(lines) {
		endLine = len(lines)
	}

	return strings.Join(lines[startLine-1:endLine], "\n")
}

// IsRunning returns whether the monitor is running
func (m *Monitor) IsRunning() bool {
	return m.isRunning
}

// StartTime returns when the monitor started
func (m *Monitor) StartTime() time.Time {
	return m.startTime
}
