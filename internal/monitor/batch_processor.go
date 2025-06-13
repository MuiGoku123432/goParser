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
	flushChan      chan struct{} // Channel to trigger immediate flush
	ctx            context.Context
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
		flushChan:      make(chan struct{}, 1),
	}
}

// Add adds a file change to the batch
func (bp *BatchProcessor) Add(change FileChange) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	// Update or add the change (latest change wins)
	bp.pendingChanges[change.Path] = change
	log.Printf("Batch processor: Added %s to batch (current size: %d)", change.Path, len(bp.pendingChanges))

	// Check if we should flush based on batch size
	if len(bp.pendingChanges) >= bp.batchSize {
		log.Printf("Batch processor: Batch size reached (%d), triggering flush", bp.batchSize)
		// Non-blocking send to trigger flush
		select {
		case bp.flushChan <- struct{}{}:
		default:
		}
	}
}

// Start starts the periodic flush routine
func (bp *BatchProcessor) Start(ctx context.Context) {
	bp.ctx = ctx
	ticker := time.NewTicker(bp.flushInterval)
	defer ticker.Stop()

	log.Printf("Batch processor started (size: %d, interval: %v)", bp.batchSize, bp.flushInterval)

	for {
		select {
		case <-ticker.C:
			bp.checkAndFlush(ctx)
		case <-bp.flushChan:
			// Immediate flush requested
			log.Println("Batch processor: Immediate flush requested")
			bp.flush(ctx)
		case <-ctx.Done():
			// Final flush before shutdown
			log.Println("Batch processor: Shutting down, final flush")
			bp.flush(ctx)
			return
		}
	}
}

// checkAndFlush checks if a flush is needed based on time
func (bp *BatchProcessor) checkAndFlush(ctx context.Context) {
	bp.mu.Lock()
	pendingCount := len(bp.pendingChanges)
	timeSinceLastFlush := time.Since(bp.lastFlush)
	shouldFlush := pendingCount > 0 && timeSinceLastFlush >= bp.flushInterval
	bp.mu.Unlock()

	if shouldFlush {
		log.Printf("Batch processor: Time-based flush (pending: %d, time since last: %v)",
			pendingCount, timeSinceLastFlush)
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

	// Extract changes and create a copy to avoid holding the lock during processing
	changes := make([]FileChange, 0, len(bp.pendingChanges))
	for _, change := range bp.pendingChanges {
		changes = append(changes, change)
	}

	// Clear pending changes and update flush time
	bp.pendingChanges = make(map[string]FileChange)
	bp.lastFlush = time.Now()
	bp.mu.Unlock()

	// Process the batch
	log.Printf("Batch processor: Flushing batch of %d changes", len(changes))
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

// ForceFlush forces an immediate flush of pending changes
func (bp *BatchProcessor) ForceFlush() {
	select {
	case bp.flushChan <- struct{}{}:
		log.Println("Batch processor: Force flush requested")
	default:
		// Channel already has a flush request pending
	}
}

// handleFailedBatch handles failed changes
func (bp *BatchProcessor) handleFailedBatch(changes []FileChange) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	retriedCount := 0
	droppedCount := 0

	for _, change := range changes {
		change.Retries++
		if change.Retries < 3 {
			// Re-add for retry
			bp.pendingChanges[change.Path] = change
			retriedCount++
		} else {
			log.Printf("Failed to process %s after %d retries, dropping", change.Path, change.Retries)
			droppedCount++
		}
	}

	if retriedCount > 0 {
		log.Printf("Batch processor: Re-queued %d failed changes for retry", retriedCount)
	}
	if droppedCount > 0 {
		log.Printf("Batch processor: Dropped %d changes after max retries", droppedCount)
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

// GetPendingCount returns the number of pending changes
func (bp *BatchProcessor) GetPendingCount() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return len(bp.pendingChanges)
}
