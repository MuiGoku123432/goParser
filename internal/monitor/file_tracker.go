// internal/monitor/file_tracker.go

package monitor

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
)

// FileTracker keeps track of file states to detect changes
type FileTracker struct {
	mu        sync.RWMutex
	states    map[string]FileState
	statePath string
}

// FileState represents the state of a file at a point in time
type FileState struct {
	Path     string `json:"path"`
	Hash     string `json:"hash"`
	Modified int64  `json:"modified"`
}

// NewFileTracker creates a new file tracker
func NewFileTracker(rootPath string) *FileTracker {
	return &FileTracker{
		states:    make(map[string]FileState),
		statePath: filepath.Join(rootPath, ".goparse_state.json"),
	}
}

// LoadState loads the saved state from disk
func (ft *FileTracker) LoadState() error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	data, err := ioutil.ReadFile(ft.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state file yet
		}
		return err
	}

	var states []FileState
	if err := json.Unmarshal(data, &states); err != nil {
		return err
	}

	for _, state := range states {
		ft.states[state.Path] = state
	}

	return nil
}

// SaveState saves the current state to disk
func (ft *FileTracker) SaveState() error {
	ft.mu.RLock()
	states := make([]FileState, 0, len(ft.states))
	for _, state := range ft.states {
		states = append(states, state)
	}
	ft.mu.RUnlock()

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(ft.statePath, data, 0644)
}

// HasChanged checks if a file has changed since last tracked
func (ft *FileTracker) HasChanged(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	hash, err := ft.calculateHash(path)
	if err != nil {
		return false, err
	}

	ft.mu.RLock()
	oldState, exists := ft.states[path]
	ft.mu.RUnlock()

	if !exists {
		return true, nil
	}

	return oldState.Hash != hash || oldState.Modified != info.ModTime().Unix(), nil
}

// UpdateState updates the tracked state of a file
func (ft *FileTracker) UpdateState(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	hash, err := ft.calculateHash(path)
	if err != nil {
		return err
	}

	ft.mu.Lock()
	ft.states[path] = FileState{
		Path:     path,
		Hash:     hash,
		Modified: info.ModTime().Unix(),
	}
	ft.mu.Unlock()

	return nil
}

// RemoveState removes a file from tracking
func (ft *FileTracker) RemoveState(path string) {
	ft.mu.Lock()
	delete(ft.states, path)
	ft.mu.Unlock()
}

// calculateHash computes the MD5 hash of a file
func (ft *FileTracker) calculateHash(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}

	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:]), nil
}

// GetAllStates returns all tracked file states
func (ft *FileTracker) GetAllStates() []FileState {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	states := make([]FileState, 0, len(ft.states))
	for _, state := range ft.states {
		states = append(states, state)
	}
	return states
}

// Clear removes all tracked states
func (ft *FileTracker) Clear() {
	ft.mu.Lock()
	ft.states = make(map[string]FileState)
	ft.mu.Unlock()
}
