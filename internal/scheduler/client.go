package scheduler

import (
	"context"
	"encoding/json"

	natscore "eve-industry-planner/internal/core/nats"

	"github.com/nats-io/nats.go/jetstream"
)

// ScheduleRequest is an alias for natscore.ScheduleRequest.
// Deprecated: Use natscore.ScheduleRequest directly instead.
type ScheduleRequest = natscore.ScheduleRequest

// PublishScheduleRequest publishes a ScheduleRequest message to JetStream.
// The request is automatically marshaled to JSON and published to the scheduler:schedule subject.
func PublishScheduleRequest(js jetstream.JetStream, req natscore.ScheduleRequest) error {
	reqData, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = js.Publish(context.Background(), natscore.SubjectSchedulerSchedule, reqData)
	return err
}

// PublishScheduleRequestWithData is a convenience function to publish a schedule request with structured data.
// It automatically marshals the data parameter to JSON and creates a ScheduleRequest.
func PublishScheduleRequestWithData(js jetstream.JetStream, taskType string, runAt int64, data interface{}) error {
	var rawData json.RawMessage
	if data != nil {
		var err error
		rawData, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	req := natscore.ScheduleRequest{
		TaskType: taskType,
		RunAt:    runAt,
		Data:     rawData,
	}
	return PublishScheduleRequest(js, req)
}

// PublishEmptyMessage publishes an EmptyMessage to the specified subject.
// Used for simple trigger messages where no data is needed.
func PublishEmptyMessage(js jetstream.JetStream, subject string) error {
	msg := natscore.EmptyMessage{}
	msgData, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = js.Publish(context.Background(), subject, msgData)
	return err
}

// PublishTaskMessage publishes a TaskMessage to the specified subject.
// Used for task triggers that need to pass arbitrary data.
func PublishTaskMessage(js jetstream.JetStream, subject string, taskType string, data interface{}) error {
	var rawData json.RawMessage
	if data != nil {
		var err error
		rawData, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	msg := natscore.TaskMessage{
		TaskType: taskType,
		Data:     rawData,
	}
	msgData, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = js.Publish(context.Background(), subject, msgData)
	return err
}
