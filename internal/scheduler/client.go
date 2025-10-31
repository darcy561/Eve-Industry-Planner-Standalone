package scheduler

import (
	"encoding/json"

	natslib "github.com/nats-io/nats.go"
)

// ScheduleRequest represents a request to schedule a task
type ScheduleRequest struct {
	JobID    string          `json:"job_id,omitempty"` // Unique job identifier (optional, will be generated if not provided)
	TaskType string          `json:"task_type"`        // e.g., "refreshSystemIndexes"
	RunAt    int64           `json:"run_at"`           // Unix timestamp in milliseconds
	Data     json.RawMessage `json:"data,omitempty"`   // Optional JSON-encoded data to pass to the task handler
}

// PublishScheduleRequest is a helper function that any service can use to publish a schedule request
// data can be nil if no data is needed
func PublishScheduleRequest(natsConn *natslib.Conn, taskType string, runAt int64, data json.RawMessage) error {
	req := ScheduleRequest{
		TaskType: taskType,
		RunAt:    runAt,
		Data:     data,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := natsConn.Publish("scheduler:schedule", reqData); err != nil {
		return err
	}
	return natsConn.Flush()
}

// PublishScheduleRequestWithData is a convenience function to publish a schedule request with structured data
// It automatically marshals the data parameter to JSON
func PublishScheduleRequestWithData(natsConn *natslib.Conn, taskType string, runAt int64, data interface{}) error {
	var rawData json.RawMessage
	if data != nil {
		var err error
		rawData, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	return PublishScheduleRequest(natsConn, taskType, runAt, rawData)
}
