// internal/monitor/batch_processor.go

package monitor

import (
	"context"
	"log"
	"sync"
	"time"
)

// BatchProcessor groups file changes for efficient processing
type BatchProcessor struct {
	mu             sync.Mutex
	pendingChanges map[string]FileChange
	batchSize      int
	flushInterval  time.Duration
	processFunc    ProcessBatchFunc
	lastFlush      time.Time
	metrics        *BatchMetrics
}

// FileChange represents a pending file change
type FileChange struct {
	Path      string
	Type      ChangeType
	Timestamp time.Time
	Retries   int
}

// ChangeType represents the type of file change
type ChangeType int

const (
	ChangeTypeCreate ChangeType = iota
	ChangeTypeModify
	ChangeTypeDelete
)

// ProcessBatchFunc is the function that processes a batch of changes
type ProcessBatchFunc func(ctx context.Context, changes []FileChange) error

// BatchMetrics tracks batch processing performance
type BatchMetrics struct {
	mu               sync.RWMutex
	TotalBatches     int64
	TotalChanges     int64
	AverageBatchSize float64
	ProcessingTime   time.Duration
	Errors           int64
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int, flushInterval time.Duration, processFunc ProcessBatchFunc) *BatchProcessor {
	return &BatchProcessor{
		pendingChanges: make(map[string]FileChange),
		batchSize:      batchSize,
		flushInterval:  flushInterval,
		processFunc:    processFunc,
		lastFlush:      time.Now(),
		metrics:        &BatchMetrics{},
	}
}

// Add adds a file change to the batch
func (bp *BatchProcessor) Add(change FileChange) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	// Update or add the change (latest change wins)
	bp.pendingChanges[change.Path] = change

	// Check if we should flush
	if len(bp.pendingChanges) >= bp.batchSize {
		go bp.flush(context.Background())
	}
}

// Start starts the periodic flush routine
func (bp *BatchProcessor) Start(ctx context.Context) {
	ticker := time.NewTicker(bp.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bp.checkAndFlush(ctx)
		case <-ctx.Done():
			// Final flush before shutdown
			bp.flush(ctx)
			return
		}
	}
}

// checkAndFlush checks if a flush is needed based on time
func (bp *BatchProcessor) checkAndFlush(ctx context.Context) {
	bp.mu.Lock()
	shouldFlush := len(bp.pendingChanges) > 0 &&
		time.Since(bp.lastFlush) >= bp.flushInterval
	bp.mu.Unlock()

	if shouldFlush {
		bp.flush(ctx)
	}
}

// flush processes all pending changes
func (bp *BatchProcessor) flush(ctx context.Context) {
	bp.mu.Lock()
	if len(bp.pendingChanges) == 0 {
		bp.mu.Unlock()
		return
	}

	// Extract changes
	changes := make([]FileChange, 0, len(bp.pendingChanges))
	for _, change := range bp.pendingChanges {
		changes = append(changes, change)
	}

	// Clear pending changes
	bp.pendingChanges = make(map[string]FileChange)
	bp.lastFlush = time.Now()
	bp.mu.Unlock()

	// Process the batch
	start := time.Now()
	if err := bp.processFunc(ctx, changes); err != nil {
		log.Printf("Batch processing error: %v", err)
		bp.handleFailedBatch(changes)
		bp.updateMetrics(len(changes), time.Since(start), true)
	} else {
		bp.updateMetrics(len(changes), time.Since(start), false)
		log.Printf("Successfully processed batch of %d changes in %v", len(changes), time.Since(start))
	}
}

// handleFailedBatch handles failed changes
func (bp *BatchProcessor) handleFailedBatch(changes []FileChange) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	for _, change := range changes {
		change.Retries++
		if change.Retries < 3 {
			// Re-add for retry
			bp.pendingChanges[change.Path] = change
		} else {
			log.Printf("Failed to process %s after %d retries", change.Path, change.Retries)
		}
	}
}

// updateMetrics updates batch processing metrics
func (bp *BatchProcessor) updateMetrics(batchSize int, duration time.Duration, isError bool) {
	bp.metrics.mu.Lock()
	defer bp.metrics.mu.Unlock()

	bp.metrics.TotalBatches++
	bp.metrics.TotalChanges += int64(batchSize)
	bp.metrics.ProcessingTime += duration

	// Update rolling average
	bp.metrics.AverageBatchSize = float64(bp.metrics.TotalChanges) / float64(bp.metrics.TotalBatches)

	if isError {
		bp.metrics.Errors++
	}
}

// GetMetrics returns current metrics
func (bp *BatchProcessor) GetMetrics() BatchMetrics {
	bp.metrics.mu.RLock()
	defer bp.metrics.mu.RUnlock()
	return *bp.metrics
}
