// internal/monitor/monitor.go

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
	graphClient    GraphClient // Changed from interface{}
	embeddingGen   *embeddings.CodeEmbeddingGenerator
	fileTracker    *FileTracker
	stopChan       chan struct{}
	wg             sync.WaitGroup
	fileHandler    func(context.Context, string)
	eventPublisher func(MonitorEvent)
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
	GraphClient  GraphClient // Changed from interface{}
	EmbeddingGen *embeddings.CodeEmbeddingGenerator
}

// NewMonitor creates a new file monitor
func NewMonitor(config Config) (*Monitor, error) {
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

	monitor.fileHandler = monitor.processFile

	// Load existing file state
	if err := monitor.fileTracker.LoadState(); err != nil {
		log.Printf("Warning: Failed to load file state: %v", err)
	}

	return monitor, nil
}

// SetEventPublisher configures a callback for publishing monitor events.
func (m *Monitor) SetEventPublisher(publisher func(MonitorEvent)) {
	m.eventPublisher = publisher
}

// Start begins monitoring for file changes
func (m *Monitor) Start(ctx context.Context) error {
	// Add directories to watch
	err := filepath.Walk(m.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// Skip certain directories
			if shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return m.watcher.Add(path)
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Start watching
	m.wg.Add(1)
	go m.watch(ctx)

	// Periodic state save
	m.wg.Add(1)
	go m.periodicStateSave()

	return nil
}

// Stop stops the monitor
func (m *Monitor) Stop() error {
	close(m.stopChan)
	m.watcher.Close()
	m.wg.Wait()

	// Save final state
	return m.fileTracker.SaveState()
}

// watch handles file system events
func (m *Monitor) watch(ctx context.Context) {
	defer m.wg.Done()

	for {
		select {
		case <-m.stopChan:
			return
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.handleEvent(ctx, event)
		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// handleEvent processes a file system event
func (m *Monitor) handleEvent(ctx context.Context, event fsnotify.Event) {
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		log.Printf("File created: %s", event.Name)
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if !shouldSkipDir(info.Name()) {
				m.watcher.Add(event.Name)
			}
			if m.eventPublisher != nil {
				m.eventPublisher(MonitorEvent{Type: "create_dir", FilePath: event.Name, Timestamp: time.Now()})
			}
			return
		}
		if !isSupportedFile(event.Name) {
			return
		}
		m.fileHandler(ctx, event.Name)
		if m.eventPublisher != nil {
			m.eventPublisher(MonitorEvent{Type: "create", FilePath: event.Name, Timestamp: time.Now()})
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		if !isSupportedFile(event.Name) {
			return
		}
		log.Printf("File modified: %s", event.Name)
		m.fileHandler(ctx, event.Name)
		if m.eventPublisher != nil {
			m.eventPublisher(MonitorEvent{Type: "modify", FilePath: event.Name, Timestamp: time.Now()})
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		log.Printf("File removed: %s", event.Name)
		m.handleRemoval(ctx, event.Name)
		if m.eventPublisher != nil {
			m.eventPublisher(MonitorEvent{Type: "remove", FilePath: event.Name, Timestamp: time.Now()})
		}

	case event.Op&fsnotify.Rename == fsnotify.Rename:
		log.Printf("File renamed: %s", event.Name)
		m.handleRemoval(ctx, event.Name)
		if m.eventPublisher != nil {
			m.eventPublisher(MonitorEvent{Type: "rename", FilePath: event.Name, Timestamp: time.Now()})
		}
	}
}

// processFile processes a single file
func (m *Monitor) processFile(ctx context.Context, filePath string) {
	// Check if file has actually changed
	changed, err := m.fileTracker.HasChanged(filePath)
	if err != nil {
		log.Printf("Error checking file change status: %v", err)
		return
	}

	if !changed {
		return
	}

	// Convert to relative path
	relPath, err := filepath.Rel(m.rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	// Parse the file
	pf, err := m.driver.Parse(filePath)
	if err != nil {
		log.Printf("Failed to parse %s: %v", relPath, err)
		return
	}

	// Update file path to relative
	pf.FilePath = relPath

	// Update graph based on client type
	switch client := m.graphClient.(type) {
	case *model.Neo4jClient:
		m.updateNeo4j(ctx, client, pf, filePath)
	case *model.AGEClient:
		m.updateAGE(ctx, client, pf, filePath)
	case *model.OracleGraphClient:
		m.updateOracle(ctx, client, pf, filePath)
	default:
		log.Printf("Unknown graph client type")
		return
	}

	// Update file tracker
	if err := m.fileTracker.UpdateState(filePath); err != nil {
		log.Printf("Failed to update file state: %v", err)
	}
}

// updateNeo4j updates Neo4j with the parsed file data
func (m *Monitor) updateNeo4j(ctx context.Context, client *model.Neo4jClient, pf driver.ParsedFile, filePath string) {
	// Update file node
	if err := client.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
		log.Printf("Failed to upsert file: %v", err)
		return
	}

	// Update all entities
	m.updateEntities(ctx, client, pf, filePath)
}

// updateAGE updates Apache AGE with the parsed file data
func (m *Monitor) updateAGE(ctx context.Context, client *model.AGEClient, pf driver.ParsedFile, filePath string) {
	// Update file node
	if err := client.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
		log.Printf("Failed to upsert file: %v", err)
		return
	}

	// Update all entities
	m.updateEntities(ctx, client, pf, filePath)
}

// updateOracle updates Oracle Graph with the parsed file data
func (m *Monitor) updateOracle(ctx context.Context, client *model.OracleGraphClient, pf driver.ParsedFile, filePath string) {
	// Update file node
	if err := client.UpsertFile(ctx, pf.FilePath, pf.Language); err != nil {
		log.Printf("Failed to upsert file: %v", err)
		return
	}

	// Update all entities
	m.updateEntities(ctx, client, pf, filePath)
}

// updateEntities updates all entities for a parsed file
func (m *Monitor) updateEntities(ctx context.Context, client GraphClient, pf driver.ParsedFile, filePath string) {
	// Update functions
	for _, fn := range pf.Funcs {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("Failed to update function %s: %v", fn.Name, err)
		}
	}

	// Update imports
	for _, imp := range pf.Imports {
		if err := client.UpsertImport(ctx, imp); err != nil {
			log.Printf("Failed to update import %s: %v", imp.Module, err)
		}
	}

	// Update other entities similarly...
	// Variables
	for _, v := range pf.Variables {
		if err := client.UpsertVariable(ctx, v); err != nil {
			log.Printf("Failed to update variable %s: %v", v.Name, err)
		}
	}

	// Types
	for _, t := range pf.Types {
		if err := client.UpsertType(ctx, t); err != nil {
			log.Printf("Failed to update type %s: %v", t.Name, err)
		}
	}

	// Interfaces
	for _, i := range pf.Interfaces {
		if err := client.UpsertInterface(ctx, i); err != nil {
			log.Printf("Failed to update interface %s: %v", i.Name, err)
		}
	}

	// Classes
	for _, c := range pf.Classes {
		if err := client.UpsertClass(ctx, c); err != nil {
			log.Printf("Failed to update class %s: %v", c.Name, err)
		}
	}

	// Constants
	for _, c := range pf.Constants {
		if err := client.UpsertConstant(ctx, c); err != nil {
			log.Printf("Failed to update constant %s: %v", c.Name, err)
		}
	}

	// JSX Elements
	for _, jsx := range pf.JSXElements {
		if err := client.UpsertJSXElement(ctx, jsx); err != nil {
			log.Printf("Failed to update JSX element %s: %v", jsx.TagName, err)
		}
	}

	// CSS Rules
	for _, css := range pf.CSSRules {
		if err := client.UpsertCSSRule(ctx, css); err != nil {
			log.Printf("Failed to update CSS rule %s: %v", css.Selector, err)
		}
	}

	// Function Calls
	for _, fc := range pf.FunctionCalls {
		if err := client.UpsertFunctionCall(ctx, fc); err != nil {
			log.Printf("Failed to update function call: %v", err)
		}
	}

	// Type Usages
	for _, tu := range pf.TypeUsages {
		if err := client.UpsertTypeUsage(ctx, tu); err != nil {
			log.Printf("Failed to update type usage: %v", err)
		}
	}

	// Extends relationships
	for _, e := range pf.Extends {
		if err := client.UpsertExtends(ctx, e); err != nil {
			log.Printf("Failed to update extends: %v", err)
		}
	}

	// Implements relationships
	for _, i := range pf.Implements {
		if err := client.UpsertImplements(ctx, i); err != nil {
			log.Printf("Failed to update implements: %v", err)
		}
	}

	// Update embeddings if generator is available
	if m.embeddingGen != nil {
		m.updateEmbeddings(ctx, pf, filePath)
	}
}

// updateEmbeddings updates embeddings for the file
func (m *Monitor) updateEmbeddings(ctx context.Context, pf driver.ParsedFile, filePath string) {
	fileContent, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Printf("Failed to read file for embeddings: %v", err)
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
		log.Printf("Failed to update embeddings: %v", err)
	}
}

// handleRemoval handles file removal
func (m *Monitor) handleRemoval(ctx context.Context, filePath string) {
	relPath, err := filepath.Rel(m.rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	// TODO: Implement removal from graph databases
	// This requires adding RemoveFile methods to each client

	// Remove from file tracker
	m.fileTracker.RemoveState(filePath)

	// Remove embeddings if available
	if m.embeddingGen != nil {
		// The embedding generator has either pgStore or oracleStore
		// We need to check which one is being used
		type embeddingStore interface {
			DeleteChunksForFile(context.Context, string) error
		}

		// Try to delete from whichever store is being used
		// This is a simplified approach - in production you'd want better access
		// to the internal stores or a method on CodeEmbeddingGenerator
		log.Printf("Note: Embedding removal not fully implemented for file: %s", relPath)
	}
}

// periodicStateSave saves the file state periodically
func (m *Monitor) periodicStateSave() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.fileTracker.SaveState(); err != nil {
				log.Printf("Failed to save file state: %v", err)
			}
		case <-m.stopChan:
			return
		}
	}
}

// Helper functions

func shouldSkipDir(name string) bool {
	skipDirs := map[string]bool{
		"node_modules": true,
		".git":         true,
		"dist":         true,
		"build":        true,
		".next":        true,
		"coverage":     true,
		"vendor":       true,
		".vscode":      true,
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
	return supportedExts[filepath.Ext(path)]
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
