package monitor

import "time"

// MonitorEvent represents a filesystem event emitted by the monitor.
type MonitorEvent struct {
	Type      string    `json:"type"`
	FilePath  string    `json:"file_path"`
	Timestamp time.Time `json:"timestamp"`
	Details   any       `json:"details,omitempty"`
}
