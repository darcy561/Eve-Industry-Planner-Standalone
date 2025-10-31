package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-co-op/gocron/v2"
	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	redislib "github.com/redis/go-redis/v9"

	"eve-industry-planner/internal/scheduler"
	taskscore "eve-industry-planner/internal/tasks"
)

// ScheduleRequest represents a request to schedule a task
type ScheduleRequest struct {
	JobID    string          `json:"job_id"`         // Unique job identifier
	TaskType string          `json:"task_type"`      // e.g., "refreshSystemIndexes"
	RunAt    int64           `json:"run_at"`         // Unix timestamp in milliseconds
	Data     json.RawMessage `json:"data,omitempty"` // Optional JSON-encoded data to pass to the task handler
}

// Ensure SchedulerHandler implements scheduler.Scheduler interface
var _ scheduler.Scheduler = (*SchedulerHandler)(nil)

// Tag constants for consistent tagging
const (
	tagJobID    = "jobID"
	tagTaskType = "taskType"
)

// SchedulerHandler manages dynamic task scheduling via messages
type SchedulerHandler struct {
	scheduler   gocron.Scheduler
	natsConn    *natslib.Conn
	jsContext   jetstream.JetStream
	redisClient *redislib.Client
	log         *slog.Logger
	handlers    map[string]scheduler.TaskHandler

	subject string
	sub     *natslib.Subscription
}

// NewSchedulerHandler creates a new scheduler message handler
func NewSchedulerHandler(natsConn *natslib.Conn, jsContext jetstream.JetStream, redisClient *redislib.Client, log *slog.Logger) (*SchedulerHandler, error) {
	sched, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("failed to create gocron scheduler: %w", err)
	}

	return &SchedulerHandler{
		scheduler:   sched,
		natsConn:    natsConn,
		jsContext:   jsContext,
		redisClient: redisClient,
		log:         log,
		handlers:    make(map[string]scheduler.TaskHandler),
		subject:     "scheduler:schedule",
	}, nil
}

// RegisterHandler registers a task handler for a specific task type
func (h *SchedulerHandler) RegisterHandler(taskType string, handler scheduler.TaskHandler) {
	h.handlers[taskType] = handler
}

// findJobByID finds a job by its jobID tag
func (h *SchedulerHandler) findJobByID(jobID string) (gocron.Job, bool) {
	jobs := h.scheduler.Jobs()
	for _, job := range jobs {
		tags := job.Tags()
		for _, tag := range tags {
			if tag == fmt.Sprintf("%s:%s", tagJobID, jobID) {
				return job, true
			}
		}
	}
	return nil, false
}

// getJobsByTaskType gets all jobs with the specified task type tag
func (h *SchedulerHandler) getJobsByTaskType(taskType string) []gocron.Job {
	jobs := h.scheduler.Jobs()
	var result []gocron.Job
	for _, job := range jobs {
		tags := job.Tags()
		for _, tag := range tags {
			if tag == fmt.Sprintf("%s:%s", tagTaskType, taskType) {
				result = append(result, job)
				break
			}
		}
	}
	return result
}

// HasScheduledJob checks if there's already a scheduled job for the given task type
func (h *SchedulerHandler) HasScheduledJob(taskType string) bool {
	jobs := h.getJobsByTaskType(taskType)
	return len(jobs) > 0
}

// isSingleJobTaskType returns true if the task type should only have one scheduled job at a time
func isSingleJobTaskType(taskType string) bool {
	singleJobTaskTypes := map[string]bool{
		taskscore.TaskTypeRefreshSystemIndexes:  true,
		taskscore.TaskTypeRefreshAdjustedPrices: true,
		// Add more task types that should only have one job here
	}
	return singleJobTaskTypes[taskType]
}

// removeJob removes a job by its jobID tag
func (h *SchedulerHandler) removeJob(jobID string, taskType string) error {
	job, exists := h.findJobByID(jobID)
	if !exists {
		return nil // Already removed
	}

	if err := h.scheduler.RemoveJob(job.ID()); err != nil {
		h.log.Warn("failed to remove job from scheduler", "job_id", jobID, "error", err)
		return err
	}

	return nil
}

// removeJobsForTaskType removes all jobs for a task type using tags
func (h *SchedulerHandler) removeJobsForTaskType(ctx context.Context, taskType string) {
	// Get jobs before removing them (for Redis cleanup)
	jobsToRemove := h.getJobsByTaskType(taskType)
	if len(jobsToRemove) == 0 {
		return
	}

	// Extract jobIDs from tags for Redis cleanup
	var jobIDs []string
	for _, job := range jobsToRemove {
		tags := job.Tags()
		for _, tag := range tags {
			if jobID := extractTagValue(tag, tagJobID); jobID != "" {
				jobIDs = append(jobIDs, jobID)
				break
			}
		}
	}

	// Use RemoveByTags to remove all jobs with this task type tag
	tag := fmt.Sprintf("%s:%s", tagTaskType, taskType)
	h.scheduler.RemoveByTags(tag)

	if len(jobsToRemove) > 0 {
		h.log.Info("removed existing jobs for task type", "task_type", taskType, "removed", len(jobsToRemove))

		// Remove from Redis
		if h.redisClient != nil {
			for _, jobID := range jobIDs {
				key := h.jobKey(taskType, jobID)
				_ = h.redisClient.Del(ctx, key).Err()
			}
		}
	}
}

// extractTagValue extracts the value from a tag in format "prefix:value"
func extractTagValue(tag, prefix string) string {
	expectedPrefix := prefix + ":"
	if len(tag) > len(expectedPrefix) && tag[:len(expectedPrefix)] == expectedPrefix {
		return tag[len(expectedPrefix):]
	}
	return ""
}

// scheduleJob schedules a one-time job to run at the specified time
func (h *SchedulerHandler) scheduleJob(ctx context.Context, req ScheduleRequest) error {
	handler, exists := h.handlers[req.TaskType]
	if !exists {
		return fmt.Errorf("no handler registered for task type: %s", req.TaskType)
	}

	runAt := time.Unix(0, req.RunAt*int64(time.Millisecond))
	now := time.Now()

	// If run_at is in the past, schedule for soon
	if !runAt.After(now) {
		h.log.Info("run_at is in the past, scheduling soon", "job_id", req.JobID, "task_type", req.TaskType, "run_at", runAt.Format(time.RFC3339), "now", now.Format(time.RFC3339))
		runAt = now.Add(5 * time.Second)
		req.RunAt = runAt.UnixMilli()
	}

	duration := time.Until(runAt)

	// Capture variables for the job callback
	jobID := req.JobID
	taskType := req.TaskType
	taskData := req.Data

	// Create the job function
	jobFunc := func() {
		// Remove job from scheduler
		h.removeJob(jobID, taskType)

		// Execute the handler
		ctx := context.Background()
		startTime := time.Now()
		h.log.Info("job started", "job_id", jobID, "task_type", taskType)

		if err := handler(ctx, taskData); err != nil {
			h.log.Error("job failed", "job_id", jobID, "task_type", taskType, "error", err, "duration_ms", time.Since(startTime).Milliseconds())
		} else {
			h.log.Info("job completed", "job_id", jobID, "task_type", taskType, "duration_ms", time.Since(startTime).Milliseconds())
		}

		// Remove from Redis after execution
		if h.redisClient != nil {
			_ = h.redisClient.Del(ctx, h.jobKey(taskType, jobID)).Err()
		}
	}

	// Schedule the job using gocron with tags
	// Tags are in format "jobID:value" and "taskType:value" for easy querying
	_, err := h.scheduler.NewJob(
		gocron.DurationJob(duration),
		gocron.NewTask(jobFunc),
		gocron.WithTags(
			fmt.Sprintf("%s:%s", tagJobID, jobID),
			fmt.Sprintf("%s:%s", tagTaskType, taskType),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to schedule job: %w", err)
	}

	h.log.Info("task scheduled", "job_id", jobID, "task_type", taskType, "next_run", runAt.Format(time.RFC3339))

	// Persist to Redis
	if h.redisClient != nil {
		if err := h.saveScheduledJob(ctx, req); err != nil {
			h.log.Warn("failed to persist scheduled job to Redis", "task_type", taskType, "error", err)
		}
	}

	return nil
}

// RestoreJobs restores persisted scheduled jobs from Redis
// Should be called after all handlers are registered
func (h *SchedulerHandler) RestoreJobs() error {
	if h.redisClient == nil {
		return nil
	}
	ctx := context.Background()
	return h.restoreScheduledJobs(ctx)
}

// Start begins listening for scheduling requests and starts the scheduler
func (h *SchedulerHandler) Start() error {
	// Start the gocron scheduler
	h.scheduler.Start()

	var err error
	h.sub, err = h.natsConn.Subscribe(h.subject, func(msg *natslib.Msg) {
		var req ScheduleRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.log.Error("failed to parse schedule request", "error", err)
			_ = msg.Ack()
			return
		}

		// Generate unique job ID if not provided
		if req.JobID == "" {
			jobID, err := h.generateJobID()
			if err != nil {
				h.log.Error("failed to generate job ID", "error", err)
				_ = msg.Ack()
				return
			}
			req.JobID = jobID
		}

		hasData := len(req.Data) > 0
		if hasData {
			h.log.Info("received schedule request", "job_id", req.JobID, "task_type", req.TaskType, "run_at", req.RunAt, "has_data", true)
		} else {
			h.log.Info("received schedule request", "job_id", req.JobID, "task_type", req.TaskType, "run_at", req.RunAt)
		}

		// Find handler for this task type
		_, exists := h.handlers[req.TaskType]
		if !exists {
			h.log.Warn("no handler registered for task type", "task_type", req.TaskType)
			_ = msg.Ack()
			return
		}

		// For single-job task types, remove existing jobs before scheduling new one
		if isSingleJobTaskType(req.TaskType) {
			ctx := context.Background()
			h.removeJobsForTaskType(ctx, req.TaskType)
		}

		// Schedule the job
		ctx := context.Background()
		if err := h.scheduleJob(ctx, req); err != nil {
			h.log.Error("failed to schedule task", "job_id", req.JobID, "task_type", req.TaskType, "error", err)
			_ = msg.Ack()
			return
		}

		_ = msg.Ack()
	})

	if err != nil {
		return err
	}

	h.log.Info("scheduler handler started", "subject", h.subject)
	return nil
}

// Stop stops listening for scheduling requests and stops the scheduler
func (h *SchedulerHandler) Stop() {
	if h.sub != nil {
		_ = h.sub.Unsubscribe()
	}

	// Stop the gocron scheduler
	if err := h.scheduler.Shutdown(); err != nil {
		h.log.Warn("error shutting down scheduler", "error", err)
	}

	// Remove all jobs from Redis
	ctx := context.Background()
	if h.redisClient != nil {
		pattern := "scheduler:job:*"
		keys, err := h.redisClient.Keys(ctx, pattern).Result()
		if err == nil {
			for _, key := range keys {
				_ = h.redisClient.Del(ctx, key).Err()
			}
		}
	}

	h.log.Info("scheduler handler stopped")
}

// jobKey returns the Redis key for a scheduled job
func (h *SchedulerHandler) jobKey(taskType string, jobID string) string {
	return fmt.Sprintf("scheduler:job:%s:%s", taskType, jobID)
}

// generateJobID generates a unique job ID
func (h *SchedulerHandler) generateJobID() (string, error) {
	// Use timestamp (nanoseconds) + random bytes for uniqueness
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(randomBytes)), nil
}

// saveScheduledJob persists a scheduled job to Redis
func (h *SchedulerHandler) saveScheduledJob(ctx context.Context, req ScheduleRequest) error {
	if h.redisClient == nil {
		return nil
	}
	key := h.jobKey(req.TaskType, req.JobID)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	// No TTL - jobs persist until executed or removed
	return h.redisClient.Set(ctx, key, data, 0).Err()
}

// restoreScheduledJobs restores persisted scheduled jobs from Redis
func (h *SchedulerHandler) restoreScheduledJobs(ctx context.Context) error {
	if h.redisClient == nil {
		return nil
	}

	// Find all scheduler:job:* keys using SCAN
	pattern := "scheduler:job:*"
	var keys []string
	var cursor uint64
	for {
		var scanKeys []string
		var err error
		scanKeys, cursor, err = h.redisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		keys = append(keys, scanKeys...)
		if cursor == 0 {
			break
		}
	}

	now := time.Now()
	restored := 0
	discarded := 0

	// Track jobs by task type to handle single-job task types
	jobsByTaskType := make(map[string][]ScheduleRequest)

	// Load all jobs from Redis
	for _, key := range keys {
		data, err := h.redisClient.Get(ctx, key).Result()
		if err != nil {
			h.log.Warn("failed to read scheduled job from Redis", "key", key, "error", err)
			continue
		}

		var req ScheduleRequest
		if err := json.Unmarshal([]byte(data), &req); err != nil {
			h.log.Warn("failed to unmarshal scheduled job from Redis", "key", key, "error", err)
			_ = h.redisClient.Del(ctx, key).Err()
			discarded++
			continue
		}

		// Check if job is in the past
		runAt := time.Unix(0, req.RunAt*int64(time.Millisecond))
		if !runAt.After(now) {
			h.log.Info("discarding scheduled job (in the past)", "job_id", req.JobID, "task_type", req.TaskType, "run_at", runAt.Format(time.RFC3339))
			_ = h.redisClient.Del(ctx, key).Err()
			discarded++
			continue
		}

		// Ensure job ID exists
		if req.JobID == "" {
			jobID, err := h.generateJobID()
			if err != nil {
				h.log.Warn("failed to generate job ID for restored job, discarding", "key", key, "error", err)
				_ = h.redisClient.Del(ctx, key).Err()
				discarded++
				continue
			}
			req.JobID = jobID
			// Update the key and save back with job ID
			newKey := h.jobKey(req.TaskType, jobID)
			data, _ := json.Marshal(req)
			_ = h.redisClient.Del(ctx, key).Err()
			_ = h.redisClient.Set(ctx, newKey, data, 0).Err()
		}

		// Check if handler exists for this task type
		_, exists := h.handlers[req.TaskType]
		if !exists {
			h.log.Warn("no handler for restored scheduled job, discarding", "job_id", req.JobID, "task_type", req.TaskType)
			_ = h.redisClient.Del(ctx, h.jobKey(req.TaskType, req.JobID)).Err()
			discarded++
			continue
		}

		// Store valid job for later processing (need to deduplicate single-job task types)
		jobsByTaskType[req.TaskType] = append(jobsByTaskType[req.TaskType], req)
	}

	// Process jobs by task type, handling single-job task types
	for taskType, jobs := range jobsByTaskType {
		// For single-job task types, keep only the one with the furthest future run_at
		if isSingleJobTaskType(taskType) && len(jobs) > 1 {
			// Find job with furthest future run_at (latest)
			furthestJob := jobs[0]
			furthestTime := time.Unix(0, furthestJob.RunAt*int64(time.Millisecond))
			for i := 1; i < len(jobs); i++ {
				jobTime := time.Unix(0, jobs[i].RunAt*int64(time.Millisecond))
				if jobTime.After(furthestTime) {
					furthestJob = jobs[i]
					furthestTime = jobTime
				}
			}
			// Remove other jobs from Redis
			for _, job := range jobs {
				if job.JobID != furthestJob.JobID {
					h.log.Info("discarding duplicate scheduled job during restore", "job_id", job.JobID, "task_type", taskType)
					_ = h.redisClient.Del(ctx, h.jobKey(taskType, job.JobID)).Err()
					discarded++
				}
			}
			// Keep only the furthest future job
			jobs = []ScheduleRequest{furthestJob}
			jobsByTaskType[taskType] = jobs
		}

		// Restore each job
		for _, req := range jobs {
			if err := h.scheduleJob(ctx, req); err != nil {
				h.log.Error("failed to restore scheduled job", "job_id", req.JobID, "task_type", taskType, "error", err)
				_ = h.redisClient.Del(ctx, h.jobKey(taskType, req.JobID)).Err()
				discarded++
				continue
			}
			h.log.Info("restored scheduled job", "job_id", req.JobID, "task_type", taskType, "next_run", time.Unix(0, req.RunAt*int64(time.Millisecond)).Format(time.RFC3339))
			restored++
		}
	}

	h.log.Info("scheduled jobs restored", "restored", restored, "discarded", discarded)
	return nil
}
