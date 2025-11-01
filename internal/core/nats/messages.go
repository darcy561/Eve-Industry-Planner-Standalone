package nats

import "encoding/json"

// ScheduleRequest represents a request to schedule a one-time task.
// Used for scheduling tasks via the scheduler:schedule subject.
type ScheduleRequest struct {
	JobID    string          `json:"job_id,omitempty"` // Unique job identifier (optional, will be generated if not provided)
	TaskType string          `json:"task_type"`        // e.g., "refreshSystemIndexes"
	RunAt    int64           `json:"run_at"`           // Unix timestamp in milliseconds
	Data     json.RawMessage `json:"data,omitempty"`   // Optional JSON-encoded data to pass to the task handler
}

// EmptyMessage represents an empty message with no payload.
// Used for simple trigger messages where no data is needed.
type EmptyMessage struct{}

// TaskMessage represents a generic task message with optional data.
// Can be used for task triggers that need to pass arbitrary data.
type TaskMessage struct {
	TaskType string          `json:"task_type"`      // Task type identifier
	Data     json.RawMessage `json:"data,omitempty"` // Optional task-specific data
}

// ErrorMessage represents an error response message.
// Used for error reporting in NATS message exchanges.
type ErrorMessage struct {
	Error   string `json:"error"`             // Error message
	Code    string `json:"code,omitempty"`    // Optional error code
	Details string `json:"details,omitempty"` // Optional error details
}

// StatusMessage represents a status or health check message.
// Used for status reporting and health checks.
type StatusMessage struct {
	Status  string `json:"status"`            // Status value (e.g., "ok", "error")
	Message string `json:"message,omitempty"` // Optional status message
	Time    int64  `json:"time,omitempty"`    // Optional timestamp in Unix milliseconds
}

// Add more message types here as needed for your application
