// internal/monitor/complete_implementation.go

package monitor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Additional implementations needed for enhanced_monitor.go

// GitIntegration provides git-based change detection
type GitIntegration struct {
	repoPath   string
	lastCommit string
}

// NewGitIntegration creates a new git integration
func NewGitIntegration(repoPath string) (*GitIntegration, error) {
	// Check if it's a git repository
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("not a git repository: %s", repoPath)
	}

	gi := &GitIntegration{
		repoPath: repoPath,
	}

	// Get current commit
	commit, err := gi.getCurrentCommit()
	if err != nil {
		return nil, err
	}
	gi.lastCommit = commit

	return gi, nil
}

// getCurrentCommit gets the current HEAD commit
func (gi *GitIntegration) getCurrentCommit() (string, error) {
	cmd := exec.Command("git", "-C", gi.repoPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GetChangedFiles returns files changed since last check
func (gi *GitIntegration) GetChangedFiles(ctx context.Context) ([]GitFileChange, error) {
	// Get current commit
	currentCommit, err := gi.getCurrentCommit()
	if err != nil {
		return nil, err
	}

	// If same commit, check for uncommitted changes
	if currentCommit == gi.lastCommit {
		return gi.getUncommittedChanges(ctx)
	}

	// Get changes between commits
	changes, err := gi.getCommitChanges(ctx, gi.lastCommit, currentCommit)
	if err != nil {
		return nil, err
	}

	gi.lastCommit = currentCommit
	return changes, nil
}

// getUncommittedChanges gets uncommitted changes
func (gi *GitIntegration) getUncommittedChanges(ctx context.Context) ([]GitFileChange, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", gi.repoPath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return gi.parseStatusOutput(output), nil
}

// getCommitChanges gets changes between two commits
func (gi *GitIntegration) getCommitChanges(ctx context.Context, oldCommit, newCommit string) ([]GitFileChange, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", gi.repoPath, "diff", "--name-status", oldCommit, newCommit)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return gi.parseDiffOutput(output), nil
}

// parseStatusOutput parses git status --porcelain output
func (gi *GitIntegration) parseStatusOutput(output []byte) []GitFileChange {
	var changes []GitFileChange

	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}

		status := string(line[0:2])
		path := string(line[3:])

		var gitStatus GitStatus
		switch {
		case strings.Contains(status, "A"):
			gitStatus = GitStatusAdded
		case strings.Contains(status, "M"):
			gitStatus = GitStatusModified
		case strings.Contains(status, "D"):
			gitStatus = GitStatusDeleted
		default:
			continue
		}

		changes = append(changes, GitFileChange{
			Path:   filepath.Join(gi.repoPath, path),
			Status: gitStatus,
		})
	}

	return changes
}

// parseDiffOutput parses git diff --name-status output
func (gi *GitIntegration) parseDiffOutput(output []byte) []GitFileChange {
	var changes []GitFileChange

	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		parts := strings.Fields(string(line))
		if len(parts) < 2 {
			continue
		}

		status := GitStatus(parts[0])
		path := parts[1]

		changes = append(changes, GitFileChange{
			Path:   filepath.Join(gi.repoPath, path),
			Status: status,
		})
	}

	return changes
}

// GitFileChange represents a file change detected by git
type GitFileChange struct {
	Path   string
	Status GitStatus
}

// GitStatus represents the git status of a file
type GitStatus string

const (
	GitStatusAdded    GitStatus = "A"
	GitStatusModified GitStatus = "M"
	GitStatusDeleted  GitStatus = "D"
	GitStatusRenamed  GitStatus = "R"
)

// MetricsCollector collects and aggregates monitor metrics
type MetricsCollector struct {
	mu sync.RWMutex

	// Counters
	filesProcessed  int64
	changesDetected int64
	errors          int64

	// Timing
	processingTimes []time.Duration
	startTime       time.Time

	// Current state
	filesMonitored int
	lastChange     time.Time
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		startTime:       time.Now(),
		processingTimes: make([]time.Duration, 0, 1000),
	}
}

// RecordFileProcessed records a file processing event
func (mc *MetricsCollector) RecordFileProcessed(duration time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.filesProcessed++
	mc.lastChange = time.Now()

	// Keep last 1000 processing times
	if len(mc.processingTimes) >= 1000 {
		mc.processingTimes = mc.processingTimes[1:]
	}
	mc.processingTimes = append(mc.processingTimes, duration)
}

// RecordError records an error
func (mc *MetricsCollector) RecordError() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.errors++
}

// RecordChange records a detected change
func (mc *MetricsCollector) RecordChange() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.changesDetected++
}

// UpdateFilesMonitored updates the count of monitored files
func (mc *MetricsCollector) UpdateFilesMonitored(count int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.filesMonitored = count
}

// GetSnapshot returns a snapshot of current metrics
func (mc *MetricsCollector) GetSnapshot() MetricsSnapshot {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var avgProcessingTime time.Duration
	if len(mc.processingTimes) > 0 {
		var total time.Duration
		for _, d := range mc.processingTimes {
			total += d
		}
		avgProcessingTime = total / time.Duration(len(mc.processingTimes))
	}

	return MetricsSnapshot{
		FilesProcessed:        mc.filesProcessed,
		ChangesDetected:       mc.changesDetected,
		Errors:                mc.errors,
		FilesMonitored:        mc.filesMonitored,
		LastChange:            mc.lastChange,
		Uptime:                time.Since(mc.startTime),
		AverageProcessingTime: avgProcessingTime,
	}
}

// MetricsSnapshot represents a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	FilesProcessed        int64         `json:"files_processed"`
	ChangesDetected       int64         `json:"changes_detected"`
	Errors                int64         `json:"errors"`
	FilesMonitored        int           `json:"files_monitored"`
	LastChange            time.Time     `json:"last_change"`
	Uptime                time.Duration `json:"uptime"`
	AverageProcessingTime time.Duration `json:"average_processing_time"`
}
