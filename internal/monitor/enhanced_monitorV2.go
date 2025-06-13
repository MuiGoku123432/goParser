// internal/monitor/enhanced_monitor_v2.go
// Alternative implementation that guarantees the enhanced handler is used

package monitor

import (
	"context"
	"goParse/internal/model"
	"log"
	"path/filepath"
	"sync"
	"time"
)

// EnhancedMonitorV2 wraps the base monitor and intercepts all file processing
type EnhancedMonitorV2 struct {
	baseMonitor    *Monitor
	batchProcessor *BatchProcessor
	diffAnalyzer   *DiffAnalyzer
	gitIntegration *GitIntegration
	metrics        *MetricsCollector

	isPaused bool
	pauseMu  sync.RWMutex

	// Store original values we need to intercept
	originalFileHandler func(context.Context, string)
	ctx                 context.Context
}

// NewEnhancedMonitorV2 creates a monitor with advanced features
func NewEnhancedMonitorV2(config EnhancedConfig) (*EnhancedMonitorV2, error) {
	// DON'T start the base monitor yet - we need to set up our handler first
	baseConfig := config.Config

	// Create a custom file handler wrapper
	var em *EnhancedMonitorV2

	// Create the enhanced monitor structure first
	em = &EnhancedMonitorV2{
		metrics: NewMetricsCollector(),
	}

	// Create the base monitor with our wrapper as the handler
	wrappedConfig := baseConfig
	baseMonitor, err := NewMonitor(wrappedConfig)
	if err != nil {
		return nil, err
	}

	em.baseMonitor = baseMonitor

	// Store the original handler and replace it
	em.originalFileHandler = baseMonitor.fileHandler
	baseMonitor.fileHandler = func(ctx context.Context, path string) {
		em.processFile(ctx, path)
	}

	// Initialize batch processor if enabled
	if config.EnableBatching {
		em.batchProcessor = NewBatchProcessor(
			config.BatchSize,
			config.BatchFlushInterval,
			em.processBatch,
		)
	}

	// Initialize diff analyzer if enabled
	if config.EnableDiffAnalysis {
		em.diffAnalyzer = NewDiffAnalyzer()
	}

	// Initialize git integration if enabled
	if config.EnableGit {
		git, err := NewGitIntegration(config.RootPath)
		if err != nil {
			log.Printf("Git integration disabled: %v", err)
		} else {
			em.gitIntegration = git
		}
	}

	log.Println("[EnhancedMonitorV2] Created with intercepted file handler")

	return em, nil
}

// Start begins enhanced monitoring
func (em *EnhancedMonitorV2) Start(ctx context.Context) error {
	em.ctx = ctx

	// Start base monitor
	if err := em.baseMonitor.Start(ctx); err != nil {
		return err
	}

	// Start batch processor
	if em.batchProcessor != nil {
		go em.batchProcessor.Start(ctx)
	}

	// Start git watcher
	if em.gitIntegration != nil {
		go em.watchGit(ctx)
	}

	// Start metrics updater
	go em.updateMetrics(ctx)

	log.Println("[EnhancedMonitorV2] Started with all components")

	return nil
}

// Stop stops the enhanced monitor
func (em *EnhancedMonitorV2) Stop() error {
	return em.baseMonitor.Stop()
}

// processFile is the enhanced file processing logic
func (em *EnhancedMonitorV2) processFile(ctx context.Context, filePath string) {
	start := time.Now()

	// Check if paused
	em.pauseMu.RLock()
	if em.isPaused {
		em.pauseMu.RUnlock()
		log.Printf("[EnhancedV2] Monitor paused, skipping: %s", filePath)
		return
	}
	em.pauseMu.RUnlock()

	log.Printf("[EnhancedV2] Processing file change: %s", filePath)

	// If batching is enabled, add to batch
	if em.batchProcessor != nil {
		log.Printf("[EnhancedV2] Adding to batch: %s", filePath)
		em.batchProcessor.Add(FileChange{
			Path:      filePath,
			Type:      ChangeTypeModify,
			Timestamp: time.Now(),
		})
		return
	}

	// Otherwise process immediately
	log.Printf("[EnhancedV2] Processing immediately: %s", filePath)
	em.processFileImmediate(ctx, filePath)

	// Record metrics
	em.metrics.RecordFileProcessed(time.Since(start))
}

// processFileImmediate processes a file immediately
func (em *EnhancedMonitorV2) processFileImmediate(ctx context.Context, filePath string) {
	// Check if file has actually changed
	changed, err := em.baseMonitor.fileTracker.HasChanged(filePath)
	if err != nil {
		log.Printf("[EnhancedV2] Error checking file change status: %v", err)
		em.metrics.RecordError()
		return
	}

	if !changed {
		log.Printf("[EnhancedV2] File hasn't changed, skipping: %s", filePath)
		return
	}

	em.metrics.RecordChange()
	log.Printf("[EnhancedV2] File changed, processing: %s", filePath)

	// Convert to relative path
	relPath, err := filepath.Rel(em.baseMonitor.rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	// Parse the file
	pf, err := em.baseMonitor.driver.Parse(filePath)
	if err != nil {
		log.Printf("[EnhancedV2] Failed to parse %s: %v", relPath, err)
		em.metrics.RecordError()
		return
	}

	// Update file path to relative
	pf.FilePath = relPath

	// If diff analysis is enabled, compute diff
	if em.diffAnalyzer != nil {
		changes, hasChanges := em.diffAnalyzer.AnalyzeChanges(filePath, pf)
		if !hasChanges {
			log.Printf("[EnhancedV2] No entity changes detected in %s", relPath)
			return
		}

		log.Printf("[EnhancedV2] Entity changes detected in %s, applying changes", relPath)
		em.applyEntityChanges(ctx, changes)
	} else {
		// Use the original file handler logic from base monitor
		log.Printf("[EnhancedV2] Updating all entities in %s", relPath)
		em.baseMonitor.updateEntities(ctx, em.baseMonitor.graphClient, pf, filePath)
	}

	// Update file tracker
	if err := em.baseMonitor.fileTracker.UpdateState(filePath); err != nil {
		log.Printf("[EnhancedV2] Failed to update file state: %v", err)
	}

	log.Printf("[EnhancedV2] Successfully processed file: %s", relPath)
}

// processBatch processes a batch of file changes
func (em *EnhancedMonitorV2) processBatch(ctx context.Context, changes []FileChange) error {
	log.Printf("[EnhancedV2] Processing batch of %d changes", len(changes))

	for _, change := range changes {
		switch change.Type {
		case ChangeTypeCreate, ChangeTypeModify:
			log.Printf("[EnhancedV2] Batch processing file: %s", change.Path)
			em.processFileImmediate(ctx, change.Path)
		case ChangeTypeDelete:
			log.Printf("[EnhancedV2] Batch processing removal: %s", change.Path)
			em.baseMonitor.handleRemoval(ctx, change.Path)
		}
	}

	return nil
}

// applyEntityChanges applies only the changed entities
func (em *EnhancedMonitorV2) applyEntityChanges(ctx context.Context, changes *EntityChanges) {
	log.Printf("[EnhancedV2] Applying entity changes for: %s", changes.FilePath)

	// Apply based on graph client type
	switch client := em.baseMonitor.graphClient.(type) {
	case *model.Neo4jClient:
		em.applyChangesToNeo4j(ctx, client, changes)
	case *model.AGEClient:
		em.applyChangesToAGE(ctx, client, changes)
	case *model.OracleGraphClient:
		em.applyChangesToOracle(ctx, client, changes)
	}
}

// applyChangesToNeo4j applies entity changes to Neo4j
func (em *EnhancedMonitorV2) applyChangesToNeo4j(ctx context.Context, client *model.Neo4jClient, changes *EntityChanges) {
	// Added functions
	for _, fn := range changes.AddedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("[EnhancedV2] Failed to add function %s: %v", fn.Name, err)
		}
	}

	// Modified functions
	for _, fn := range changes.ModifiedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("[EnhancedV2] Failed to update function %s: %v", fn.Name, err)
		}
	}

	// Similar for other entity types...
}

// applyChangesToAGE applies entity changes to Apache AGE
func (em *EnhancedMonitorV2) applyChangesToAGE(ctx context.Context, client *model.AGEClient, changes *EntityChanges) {
	// Similar implementation
	for _, fn := range changes.AddedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("[EnhancedV2] Failed to add function %s: %v", fn.Name, err)
		}
	}

	// Process other entity types...
}

// applyChangesToOracle applies entity changes to Oracle Graph
func (em *EnhancedMonitorV2) applyChangesToOracle(ctx context.Context, client *model.OracleGraphClient, changes *EntityChanges) {
	// Similar implementation
	for _, fn := range changes.AddedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("[EnhancedV2] Failed to add function %s: %v", fn.Name, err)
		}
	}

	// Process other entity types...
}

// watchGit monitors git for changes
func (em *EnhancedMonitorV2) watchGit(ctx context.Context) {
	if em.gitIntegration == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("[EnhancedV2] Git watcher started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changes, err := em.gitIntegration.GetChangedFiles(ctx)
			if err != nil {
				log.Printf("[EnhancedV2] Git check error: %v", err)
				continue
			}

			if len(changes) > 0 {
				log.Printf("[EnhancedV2] Git detected %d file changes", len(changes))
			}

			for _, change := range changes {
				if !isSupportedFile(change.Path) {
					continue
				}

				switch change.Status {
				case GitStatusAdded, GitStatusModified:
					log.Printf("[EnhancedV2] Git change: %s (%s)", change.Path, change.Status)
					em.processFile(ctx, change.Path)
				case GitStatusDeleted:
					log.Printf("[EnhancedV2] Git deletion: %s", change.Path)
					em.baseMonitor.handleRemoval(ctx, change.Path)
				}
			}
		}
	}
}

// updateMetrics periodically updates system metrics
func (em *EnhancedMonitorV2) updateMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count := len(em.baseMonitor.fileTracker.GetAllStates())
			em.metrics.UpdateFilesMonitored(count)

			snapshot := em.metrics.GetSnapshot()
			log.Printf("[EnhancedV2] Metrics - Files: %d, Processed: %d, Changes: %d, Errors: %d",
				snapshot.FilesMonitored,
				snapshot.FilesProcessed,
				snapshot.ChangesDetected,
				snapshot.Errors)
		}
	}
}

// Public methods that delegate to base monitor or provide enhanced functionality

func (em *EnhancedMonitorV2) Pause() {
	em.pauseMu.Lock()
	defer em.pauseMu.Unlock()
	em.isPaused = true
	log.Println("[EnhancedV2] Monitor paused")
}

func (em *EnhancedMonitorV2) Resume() {
	em.pauseMu.Lock()
	defer em.pauseMu.Unlock()
	em.isPaused = false
	log.Println("[EnhancedV2] Monitor resumed")
}

func (em *EnhancedMonitorV2) IsPaused() bool {
	em.pauseMu.RLock()
	defer em.pauseMu.RUnlock()
	return em.isPaused
}

func (em *EnhancedMonitorV2) GetStats() MonitorStats {
	snapshot := em.metrics.GetSnapshot()

	stats := MonitorStats{
		FilesMonitored:        snapshot.FilesMonitored,
		FilesProcessed:        snapshot.FilesProcessed,
		ChangesDetected:       snapshot.ChangesDetected,
		Errors:                snapshot.Errors,
		LastChange:            snapshot.LastChange,
		AverageProcessingTime: snapshot.AverageProcessingTime,
	}

	if em.batchProcessor != nil {
		batchMetrics := em.batchProcessor.GetMetrics()
		stats.BatchMetrics = &batchMetrics
	}

	if em.diffAnalyzer != nil {
		stats.CacheSize = em.diffAnalyzer.GetCacheSize()
	}

	return stats
}

func (em *EnhancedMonitorV2) IsRunning() bool {
	return em.baseMonitor.IsRunning()
}

func (em *EnhancedMonitorV2) StartTime() time.Time {
	return em.baseMonitor.StartTime()
}

func (em *EnhancedMonitorV2) GetMonitoredFiles() []string {
	states := em.baseMonitor.fileTracker.GetAllStates()
	files := make([]string, len(states))
	for i, state := range states {
		files[i] = state.Path
	}
	return files
}

func (em *EnhancedMonitorV2) SetEventPublisher(publisher func(MonitorEvent)) {
	em.baseMonitor.SetEventPublisher(publisher)
}
