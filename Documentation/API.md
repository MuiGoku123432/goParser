# Enhanced Monitor API Reference

This document describes the HTTP and WebSocket API provided by the enhanced monitor. Start the monitor with the API enabled using `-api-port` (default `8080`).

```bash
./goparse-monitor-enhanced -root ./my-project -api-port 8080
```

The API exposes JSON endpoints for status, statistics, file information and control commands as well as a WebSocket stream for real-time events.

## REST Endpoints

| Method & Endpoint | Description | Response Fields |
|------------------|-------------|-----------------|
| **GET `/api/v1/status`** | Current monitor state | `running` (bool), `paused` (bool), `start_time` (timestamp), `version` |
| **GET `/api/v1/stats`** | Monitoring statistics | See `MonitorStats` fields below; includes optional `batch_metrics` |
| **GET `/api/v1/files`** | List of all monitored files | `total` (int), `files` ([]string) |
| **GET `/api/v1/file/{path}`** | Info on a specific file | `path`, `monitored` (bool), `timestamp` |
| **GET `/api/v1/changes`** | Summary of the most recent change | `last_change`, `changes_detected`, `files_processed` |
| **POST `/api/v1/rescan`** | Trigger a rescan (placeholder) | `status`, `path`, `force`, `timestamp` |
| **POST `/api/v1/pause`** | Pause monitoring | `status` (`"paused"`), `timestamp` |
| **POST `/api/v1/resume`** | Resume monitoring | `status` (`"resumed"`), `timestamp` |
| **`/ws/events`** | WebSocket stream of monitor events | `MonitorEvent` objects |

### MonitorStats Fields

The `/api/v1/stats` endpoint returns the following metrics:

```
files_monitored        int
files_processed        int64
changes_detected       int64
errors                int64
last_change            time.Time
average_processing_time time.Duration
batch_metrics          *BatchMetrics (optional)
cache_size             int (optional)
```

`BatchMetrics` contains:

```
TotalBatches     int64
TotalChanges     int64
AverageBatchSize float64
ProcessingTime   time.Duration
Errors           int64
```

### WebSocket Events

Connect to `ws://<host>:<port>/ws/events` to receive real-time notifications. Events have the following structure:

```
type MonitorEvent struct {
    Type      string    `json:"type"`
    FilePath  string    `json:"file_path"`
    Timestamp time.Time `json:"timestamp"`
    Details   any       `json:"details,omitempty"`
}
```

An initial event of type `"connected"` is sent upon connection.

### Example Responses

**Status**
```json
{
  "running": true,
  "paused": false,
  "start_time": "2024-01-15T10:30:00Z",
  "version": "1.0.0"
}
```

**Stats**
```json
{
  "files_monitored": 42,
  "files_processed": 42,
  "changes_detected": 0,
  "errors": 0,
  "last_change": "0001-01-01T00:00:00Z",
  "processing_time": 0,
  "batch_metrics": null,
  "cache_size": 42
}
```

**WebSocket Event**
```json
{
  "type": "connected",
  "timestamp": "2024-01-15T10:30:00Z",
  "details": { "message": "Connected to monitor events" }
}
```
