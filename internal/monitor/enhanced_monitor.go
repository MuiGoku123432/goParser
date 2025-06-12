// internal/monitor/enhanced_monitor.go

package monitor

import (
	"context"
	"goParse/internal/driver"
	"goParse/internal/model"
	"log"
	"path/filepath"
	"sync"
	"time"
)

// EnhancedMonitor is an advanced version with all features integrated
type EnhancedMonitor struct {
	*Monitor // Embed base monitor

	// Advanced components
	batchProcessor *BatchProcessor
	diffAnalyzer   *DiffAnalyzer
	gitIntegration *GitIntegration
	metrics        *MetricsCollector

	// Control
	isPaused bool
	pauseMu  sync.RWMutex
}

// EnhancedConfig extends the basic config
type EnhancedConfig struct {
	Config
	EnableBatching     bool
	BatchSize          int
	BatchFlushInterval time.Duration
	EnableGit          bool
	EnableDiffAnalysis bool
}

// NewEnhancedMonitor creates a monitor with advanced features
func NewEnhancedMonitor(config EnhancedConfig) (*EnhancedMonitor, error) {
	// Create base monitor
	baseMonitor, err := NewMonitor(config.Config)
	if err != nil {
		return nil, err
	}

	em := &EnhancedMonitor{
		Monitor: baseMonitor,
		metrics: NewMetricsCollector(),
	}

	em.fileHandler = em.processFile

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

	return em, nil
}

// Start begins enhanced monitoring
func (em *EnhancedMonitor) Start(ctx context.Context) error {
	// Start base monitor
	if err := em.Monitor.Start(ctx); err != nil {
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

	return nil
}

// processFile overrides base implementation to add advanced features
func (em *EnhancedMonitor) processFile(ctx context.Context, filePath string) {
	start := time.Now()

	// Check if paused
	em.pauseMu.RLock()
	if em.isPaused {
		em.pauseMu.RUnlock()
		return
	}
	em.pauseMu.RUnlock()

	// If batching is enabled, add to batch
	if em.batchProcessor != nil {
		em.batchProcessor.Add(FileChange{
			Path:      filePath,
			Type:      ChangeTypeModify,
			Timestamp: time.Now(),
		})
		return
	}

	// Otherwise process immediately
	em.processFileImmediate(ctx, filePath)

	// Record metrics
	em.metrics.RecordFileProcessed(time.Since(start))
}

// processFileImmediate processes a file immediately
func (em *EnhancedMonitor) processFileImmediate(ctx context.Context, filePath string) {
	// Check if file has actually changed
	changed, err := em.fileTracker.HasChanged(filePath)
	if err != nil {
		log.Printf("Error checking file change status: %v", err)
		em.metrics.RecordError()
		return
	}

	if !changed {
		return
	}

	em.metrics.RecordChange()

	// Convert to relative path
	relPath, err := filepath.Rel(em.rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	// Parse the file
	pf, err := em.driver.Parse(filePath)
	if err != nil {
		log.Printf("Failed to parse %s: %v", relPath, err)
		em.metrics.RecordError()
		return
	}

	// Update file path to relative
	pf.FilePath = relPath

	// If diff analysis is enabled, compute diff
	if em.diffAnalyzer != nil {
		changes, hasChanges := em.diffAnalyzer.AnalyzeChanges(filePath, pf)
		if !hasChanges {
			log.Printf("No entity changes detected in %s", relPath)
			return
		}

		// Apply only the changes
		em.applyEntityChanges(ctx, changes)
	} else {
		// Apply all entities (original behavior)
		em.updateAllEntities(ctx, pf, filePath)
	}

	// Update file tracker
	if err := em.fileTracker.UpdateState(filePath); err != nil {
		log.Printf("Failed to update file state: %v", err)
	}
}

// updateAllEntities updates all entities in a parsed file
func (em *EnhancedMonitor) updateAllEntities(ctx context.Context, pf driver.ParsedFile, filePath string) {
	// This is the same as the original updateEntities method from the base monitor
	em.updateEntities(ctx, em.graphClient, pf, filePath)
}

// processBatch processes a batch of file changes
func (em *EnhancedMonitor) processBatch(ctx context.Context, changes []FileChange) error {
	log.Printf("Processing batch of %d changes", len(changes))

	for _, change := range changes {
		switch change.Type {
		case ChangeTypeCreate, ChangeTypeModify:
			em.processFileImmediate(ctx, change.Path)
		case ChangeTypeDelete:
			em.handleRemoval(ctx, change.Path)
		}
	}

	return nil
}

// applyEntityChanges applies only the changed entities
func (em *EnhancedMonitor) applyEntityChanges(ctx context.Context, changes *EntityChanges) {
	// This is much more efficient than updating everything

	// Apply based on graph client type
	switch client := em.graphClient.(type) {
	case *model.Neo4jClient:
		em.applyChangesToNeo4j(ctx, client, changes)
	case *model.AGEClient:
		em.applyChangesToAGE(ctx, client, changes)
	case *model.OracleGraphClient:
		em.applyChangesToOracle(ctx, client, changes)
	}
}

// applyChangesToNeo4j applies entity changes to Neo4j
func (em *EnhancedMonitor) applyChangesToNeo4j(ctx context.Context, client *model.Neo4jClient, changes *EntityChanges) {
	// Added functions
	for _, fn := range changes.AddedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("Failed to add function %s: %v", fn.Name, err)
		}
	}

	// Modified functions
	for _, fn := range changes.ModifiedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("Failed to update function %s: %v", fn.Name, err)
		}
	}

	// Removed functions - need to implement RemoveFunction
	for _, fn := range changes.RemovedFunctions {
		log.Printf("Function removal not implemented: %s", fn.Name)
		// TODO: client.RemoveFunction(ctx, fn.Name, fn.FilePath)
	}

	// Similar for other entity types...
}

// applyChangesToAGE applies entity changes to Apache AGE
func (em *EnhancedMonitor) applyChangesToAGE(ctx context.Context, client *model.AGEClient, changes *EntityChanges) {
	// Similar to Neo4j implementation
	// Added functions
	for _, fn := range changes.AddedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("Failed to add function %s: %v", fn.Name, err)
		}
	}

	// Modified functions
	for _, fn := range changes.ModifiedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("Failed to update function %s: %v", fn.Name, err)
		}
	}

	// Process other entity types similarly...
	// TODO: Implement removal methods in AGE client
}

// applyChangesToOracle applies entity changes to Oracle Graph
func (em *EnhancedMonitor) applyChangesToOracle(ctx context.Context, client *model.OracleGraphClient, changes *EntityChanges) {
	// Similar to Neo4j implementation
	// Added functions
	for _, fn := range changes.AddedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("Failed to add function %s: %v", fn.Name, err)
		}
	}

	// Modified functions
	for _, fn := range changes.ModifiedFunctions {
		if err := client.UpsertFunction(ctx, fn); err != nil {
			log.Printf("Failed to update function %s: %v", fn.Name, err)
		}
	}

	// Process other entity types similarly...
	// TODO: Implement removal methods in Oracle client
}

// watchGit monitors git for changes
func (em *EnhancedMonitor) watchGit(ctx context.Context) {
	if em.gitIntegration == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changes, err := em.gitIntegration.GetChangedFiles(ctx)
			if err != nil {
				log.Printf("Git check error: %v", err)
				continue
			}

			for _, change := range changes {
				// Only process supported files
				if !isSupportedFile(change.Path) {
					continue
				}

				switch change.Status {
				case GitStatusAdded, GitStatusModified:
					em.processFile(ctx, change.Path)
				case GitStatusDeleted:
					em.handleRemoval(ctx, change.Path)
				}
			}
		}
	}
}

// updateMetrics periodically updates system metrics
func (em *EnhancedMonitor) updateMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Update monitored files count
			count := len(em.fileTracker.GetAllStates())
			em.metrics.UpdateFilesMonitored(count)

			// Could also update memory usage, goroutines, etc.
		}
	}
}

// Pause pauses file processing
func (em *EnhancedMonitor) Pause() {
	em.pauseMu.Lock()
	defer em.pauseMu.Unlock()
	em.isPaused = true
	log.Println("Monitor paused")
}

// Resume resumes file processing
func (em *EnhancedMonitor) Resume() {
	em.pauseMu.Lock()
	defer em.pauseMu.Unlock()
	em.isPaused = false
	log.Println("Monitor resumed")
}

// IsPaused returns whether the monitor is paused
func (em *EnhancedMonitor) IsPaused() bool {
	em.pauseMu.RLock()
	defer em.pauseMu.RUnlock()
	return em.isPaused
}

// GetStats returns monitoring statistics
func (em *EnhancedMonitor) GetStats() MonitorStats {
	snapshot := em.metrics.GetSnapshot()

	stats := MonitorStats{
		FilesMonitored:        snapshot.FilesMonitored,
		FilesProcessed:        snapshot.FilesProcessed,
		ChangesDetected:       snapshot.ChangesDetected,
		Errors:                snapshot.Errors,
		LastChange:            snapshot.LastChange,
		AverageProcessingTime: snapshot.AverageProcessingTime,
	}

	// Add batch metrics if available
	if em.batchProcessor != nil {
		batchMetrics := em.batchProcessor.GetMetrics()
		stats.BatchMetrics = &batchMetrics
	}

	// Add diff analyzer cache size
	if em.diffAnalyzer != nil {
		stats.CacheSize = em.diffAnalyzer.GetCacheSize()
	}

	return stats
}

// MonitorStats contains monitoring statistics
type MonitorStats struct {
	FilesMonitored        int           `json:"files_monitored"`
	FilesProcessed        int64         `json:"files_processed"`
	ChangesDetected       int64         `json:"changes_detected"`
	Errors                int64         `json:"errors"`
	LastChange            time.Time     `json:"last_change"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
	BatchMetrics          *BatchMetrics `json:"batch_metrics,omitempty"`
	CacheSize             int           `json:"cache_size,omitempty"`
}

// IsRunning returns whether the monitor is running
func (em *EnhancedMonitor) IsRunning() bool {
	// Implementation depends on how you track running state
	return true
}

// StartTime returns when the monitor started
func (em *EnhancedMonitor) StartTime() time.Time {
	return em.metrics.startTime
}

// GetMonitoredFiles returns list of monitored files
func (em *EnhancedMonitor) GetMonitoredFiles() []string {
	states := em.fileTracker.GetAllStates()
	files := make([]string, len(states))
	for i, state := range states {
		files[i] = state.Path
	}
	return files
}
