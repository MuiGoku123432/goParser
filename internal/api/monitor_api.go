// internal/api/monitor_api.go

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"goParse/internal/monitor"
)

// MonitorAPI provides HTTP API for the monitor
type MonitorAPI struct {
	monitor  *monitor.EnhancedMonitor
	router   *mux.Router
	upgrader websocket.Upgrader
	events   chan monitor.MonitorEvent
}

// NewMonitorAPI creates a new monitor API server
func NewMonitorAPI(m *monitor.EnhancedMonitor) *MonitorAPI {
	api := &MonitorAPI{
		monitor: m,
		router:  mux.NewRouter(),
		upgrader: websocket.Upgrader{
			// Allow WebSocket connections from any origin since HTTP
			// requests are also CORS enabled.
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		events: make(chan monitor.MonitorEvent, 100),
	}

	api.setupRoutes()
	// Apply CORS middleware so the API can be called from the web viewer.
	api.router.Use(corsMiddleware)
	// Forward monitor events to connected WebSocket clients
	api.monitor.SetEventPublisher(api.PublishEvent)
	return api
}

func (api *MonitorAPI) setupRoutes() {
	api.router.HandleFunc("/api/v1/status", api.handleStatus).Methods("GET")
	api.router.HandleFunc("/api/v1/stats", api.handleStats).Methods("GET")
	api.router.HandleFunc("/api/v1/files", api.handleListFiles).Methods("GET")
	api.router.HandleFunc("/api/v1/file/{path:.*}", api.handleFileInfo).Methods("GET")
	api.router.HandleFunc("/api/v1/changes", api.handleRecentChanges).Methods("GET")
	api.router.HandleFunc("/api/v1/rescan", api.handleRescan).Methods("POST")
	api.router.HandleFunc("/api/v1/pause", api.handlePause).Methods("POST")
	api.router.HandleFunc("/api/v1/resume", api.handleResume).Methods("POST")
	api.router.HandleFunc("/ws/events", api.handleWebSocket)
}

// handleStatus returns the monitor status
func (api *MonitorAPI) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"running":    api.monitor.IsRunning(),
		"paused":     api.monitor.IsPaused(),
		"start_time": api.monitor.StartTime(),
		"version":    "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleStats returns monitoring statistics
func (api *MonitorAPI) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := api.monitor.GetStats()

	response := map[string]interface{}{
		"files_monitored":  stats.FilesMonitored,
		"files_processed":  stats.FilesProcessed,
		"changes_detected": stats.ChangesDetected,
		"errors":           stats.Errors,
		"last_change":      stats.LastChange,
		"processing_time":  stats.AverageProcessingTime,
		"batch_metrics":    stats.BatchMetrics,
		"cache_size":       stats.CacheSize,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleListFiles returns list of monitored files
func (api *MonitorAPI) handleListFiles(w http.ResponseWriter, r *http.Request) {
	files := api.monitor.GetMonitoredFiles()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total": len(files),
		"files": files,
	})
}

// handleFileInfo returns information about a specific file
func (api *MonitorAPI) handleFileInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filePath := vars["path"]

	// Get file states from the monitor
	files := api.monitor.GetMonitoredFiles()

	// Check if file is being monitored
	found := false
	for _, file := range files {
		if file == filePath {
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Return basic file info
	response := map[string]interface{}{
		"path":      filePath,
		"monitored": true,
		"timestamp": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRecentChanges returns recent file changes
func (api *MonitorAPI) handleRecentChanges(w http.ResponseWriter, r *http.Request) {
	// Get stats which includes last change info
	stats := api.monitor.GetStats()

	response := map[string]interface{}{
		"last_change":      stats.LastChange,
		"changes_detected": stats.ChangesDetected,
		"files_processed":  stats.FilesProcessed,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRescan triggers a rescan of files
func (api *MonitorAPI) handleRescan(w http.ResponseWriter, r *http.Request) {
	// Parse request body if provided
	var req struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
	}

	// Try to decode body, but don't fail if empty
	_ = json.NewDecoder(r.Body).Decode(&req)

	// TODO: Implement actual rescan functionality
	// For now, just return acknowledgment
	response := map[string]interface{}{
		"status":    "rescan initiated",
		"path":      req.Path,
		"force":     req.Force,
		"timestamp": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handlePause pauses the monitor
func (api *MonitorAPI) handlePause(w http.ResponseWriter, r *http.Request) {
	api.monitor.Pause()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "paused",
		"timestamp": time.Now(),
	})
}

// handleResume resumes the monitor
func (api *MonitorAPI) handleResume(w http.ResponseWriter, r *http.Request) {
	api.monitor.Resume()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "resumed",
		"timestamp": time.Now(),
	})
}

// handleWebSocket handles WebSocket connections for real-time events
func (api *MonitorAPI) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := api.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// Send initial connection event
	welcomeEvent := monitor.MonitorEvent{
		Type:      "connected",
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"message": "Connected to monitor events",
		},
	}

	if err := conn.WriteJSON(welcomeEvent); err != nil {
		return
	}

	// Send events to client
	for event := range api.events {
		if err := conn.WriteJSON(event); err != nil {
			break
		}
	}
}

// PublishEvent publishes an event to WebSocket clients
func (api *MonitorAPI) PublishEvent(event monitor.MonitorEvent) {
	select {
	case api.events <- event:
		// Event sent
	default:
		// Channel full, drop event
		// In production, you might want to log this
	}
}

// Serve starts the API server
func (api *MonitorAPI) Serve(addr string) error {
	return http.ListenAndServe(addr, api.router)
}

// ServeWithServer starts the API server with a custom http.Server
func (api *MonitorAPI) ServeWithServer(server *http.Server) error {
	server.Handler = api.router
	return server.ListenAndServe()
}
