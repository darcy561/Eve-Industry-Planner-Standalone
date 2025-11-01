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

	natscore "eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/scheduler"
)

// Requestable task types - tasks that can be scheduled via message requests
var requestableTaskTypes = map[string]bool{
	// ESI refresh tasks - can be rescheduled when rate limited
	"refreshSystemIndexes":  true,
	"refreshAdjustedPrices": true,
}

// OneTimeJob represents a one-time scheduled job
type OneTimeJob struct {
	JobID    string          `json:"job_id"`
	TaskType string          `json:"task_type"`
	RunAt    int64           `json:"run_at"` // Unix timestamp in milliseconds
	Data     json.RawMessage `json:"data,omitempty"`
}

// TaskScheduler manages static cron jobs and one-time scheduled tasks
type TaskScheduler struct {
	scheduler   gocron.Scheduler
	jsContext   jetstream.JetStream
	redisClient *redislib.Client
	natsConn    *natslib.Conn
	log         *slog.Logger
	handlers    map[string]scheduler.TaskHandler
	consumer    jetstream.Consumer

	// Track one-time jobs by job ID for easy removal
	oneTimeJobs map[string]gocron.Job

	// Stop channel for message processing loop
	stopChan chan struct{}
}

// NewTaskScheduler creates a new task scheduler for cron jobs and one-time scheduled tasks
func NewTaskScheduler(jsContext jetstream.JetStream, redisClient *redislib.Client, natsConn *natslib.Conn, log *slog.Logger) (*TaskScheduler, error) {
	sched, err := gocron.NewScheduler()
	if err != nil {
		return nil, err
	}

	return &TaskScheduler{
		scheduler:   sched,
		jsContext:   jsContext,
		redisClient: redisClient,
		natsConn:    natsConn,
		log:         log,
		handlers:    make(map[string]scheduler.TaskHandler),
		oneTimeJobs: make(map[string]gocron.Job),
		stopChan:    make(chan struct{}),
	}, nil
}

// RegisterHandler registers a task handler for a specific task type
func (s *TaskScheduler) RegisterHandler(taskType string, handler scheduler.TaskHandler) {
	s.handlers[taskType] = handler
}

// HasScheduledJob checks if there's already a scheduled job for the given task type
// For static cron jobs, this always returns true after scheduling
func (s *TaskScheduler) HasScheduledJob(taskType string) bool {
	jobs := s.scheduler.Jobs()
	for _, job := range jobs {
		tags := job.Tags()
		for _, tag := range tags {
			if tag == taskType || tag == "cron:"+taskType {
				return true
			}
		}
	}
	return false
}

// ScheduleCronJob schedules a recurring cron job for a task type
// These are not requestable and not persisted
func (s *TaskScheduler) ScheduleCronJob(cronExpr string, taskType string) error {
	handler, exists := s.handlers[taskType]
	if !exists {
		return nil // Handler not registered yet, will be registered later
	}

	jobFunc := func() {
		ctx := context.Background()
		startTime := time.Now()
		jobID := fmt.Sprintf("cron-%s-%d", taskType, startTime.UnixNano())
		s.log.Info("cron job triggered", "job_id", jobID, "task_type", taskType, "cron_expr", cronExpr)

		if err := handler(ctx, nil); err != nil {
			s.log.Error("cron job handler failed", "job_id", jobID, "task_type", taskType, "error", err, "duration_ms", time.Since(startTime).Milliseconds())
		} else {
			s.log.Info("cron job handler completed", "job_id", jobID, "task_type", taskType, "duration_ms", time.Since(startTime).Milliseconds())
		}
	}

	_, err := s.scheduler.NewJob(
		gocron.CronJob(cronExpr, false),
		gocron.NewTask(jobFunc),
		gocron.WithTags("cron:"+taskType), // Tag with "cron:" prefix to distinguish from one-time jobs
	)
	if err != nil {
		return err
	}

	s.log.Info("cron job scheduled", "task_type", taskType, "cron", cronExpr)
	return nil
}

// ScheduleOneTimeJob schedules a one-time job to run at the specified time
func (s *TaskScheduler) ScheduleOneTimeJob(jobID string, taskType string, runAt time.Time, data json.RawMessage) error {
	// Check if task type is requestable
	if !requestableTaskTypes[taskType] {
		return fmt.Errorf("task type %s is not requestable", taskType)
	}

	// Check if handler exists
	handler, exists := s.handlers[taskType]
	if !exists {
		return fmt.Errorf("no handler registered for task type: %s", taskType)
	}

	now := time.Now()
	if !runAt.After(now) {
		// If run_at is in the past, schedule for soon
		s.log.Info("run_at is in the past, scheduling soon", "job_id", jobID, "task_type", taskType, "run_at", runAt.Format(time.RFC3339))
		runAt = now.Add(5 * time.Second)
	}

	duration := time.Until(runAt)
	if duration < 0 {
		duration = 5 * time.Second
	}

	// Capture variables for the job callback
	jobIDCopy := jobID
	taskTypeCopy := taskType
	taskDataCopy := data

	// Create the job function
	jobFunc := func() {
		ctx := context.Background()
		startTime := time.Now()
		s.log.Info("one-time job started", "job_id", jobIDCopy, "task_type", taskTypeCopy)

		// Execute the handler
		if err := handler(ctx, taskDataCopy); err != nil {
			s.log.Error("one-time job failed", "job_id", jobIDCopy, "task_type", taskTypeCopy, "error", err, "duration_ms", time.Since(startTime).Milliseconds())
		} else {
			s.log.Info("one-time job completed", "job_id", jobIDCopy, "task_type", taskTypeCopy, "duration_ms", time.Since(startTime).Milliseconds())
		}

		// Remove job from scheduler and Redis after execution
		s.removeOneTimeJob(jobIDCopy, taskTypeCopy)
	}

	// Schedule the job using gocron
	job, err := s.scheduler.NewJob(
		gocron.DurationJob(duration),
		gocron.NewTask(jobFunc),
		gocron.WithTags("onetime:"+jobIDCopy, "task:"+taskTypeCopy),
	)
	if err != nil {
		return fmt.Errorf("failed to schedule one-time job: %w", err)
	}

	// Track the job
	s.oneTimeJobs[jobIDCopy] = job

	// Persist to Redis
	if s.redisClient != nil {
		oneTimeJob := OneTimeJob{
			JobID:    jobIDCopy,
			TaskType: taskTypeCopy,
			RunAt:    runAt.UnixMilli(),
			Data:     taskDataCopy,
		}
		if err := s.saveOneTimeJobToRedis(context.Background(), oneTimeJob); err != nil {
			s.log.Warn("failed to persist one-time job to Redis", "job_id", jobIDCopy, "error", err)
		}
	}

	s.log.Info("one-time job scheduled", "job_id", jobIDCopy, "task_type", taskTypeCopy, "run_at", runAt.Format(time.RFC3339))
	return nil
}

// removeOneTimeJob removes a one-time job from the scheduler and Redis
func (s *TaskScheduler) removeOneTimeJob(jobID string, taskType string) {
	// Remove from scheduler
	if job, exists := s.oneTimeJobs[jobID]; exists {
		if err := s.scheduler.RemoveJob(job.ID()); err != nil {
			s.log.Warn("failed to remove one-time job from scheduler", "job_id", jobID, "error", err)
		}
		delete(s.oneTimeJobs, jobID)
	}

	// Remove from Redis
	if s.redisClient != nil {
		ctx := context.Background()
		key := s.oneTimeJobKey(jobID)
		if err := s.redisClient.Del(ctx, key).Err(); err != nil {
			s.log.Warn("failed to remove one-time job from Redis", "job_id", jobID, "error", err)
		}
	}
}

// saveOneTimeJobToRedis persists a one-time job to Redis
func (s *TaskScheduler) saveOneTimeJobToRedis(ctx context.Context, job OneTimeJob) error {
	key := s.oneTimeJobKey(job.JobID)
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	// No TTL - jobs persist until executed or removed
	return s.redisClient.Set(ctx, key, data, 0).Err()
}

// oneTimeJobKey returns the Redis key for a one-time job
func (s *TaskScheduler) oneTimeJobKey(jobID string) string {
	return fmt.Sprintf("scheduler:onetime:%s", jobID)
}

// generateJobID generates a unique job ID
func (s *TaskScheduler) generateJobID() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(randomBytes)), nil
}

// RestoreOneTimeJobs restores persisted one-time jobs from Redis
func (s *TaskScheduler) RestoreOneTimeJobs() error {
	if s.redisClient == nil {
		return nil
	}

	ctx := context.Background()
	pattern := "scheduler:onetime:*"
	var keys []string
	var cursor uint64

	// Scan for all one-time job keys
	for {
		var scanKeys []string
		var err error
		scanKeys, cursor, err = s.redisClient.Scan(ctx, cursor, pattern, 100).Result()
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

	// Load all jobs from Redis
	for _, key := range keys {
		data, err := s.redisClient.Get(ctx, key).Result()
		if err != nil {
			s.log.Warn("failed to read one-time job from Redis", "key", key, "error", err)
			continue
		}

		var job OneTimeJob
		if err := json.Unmarshal([]byte(data), &job); err != nil {
			s.log.Warn("failed to unmarshal one-time job from Redis", "key", key, "error", err)
			_ = s.redisClient.Del(ctx, key).Err()
			discarded++
			continue
		}

		// Check if job is in the past
		runAt := time.Unix(0, job.RunAt*int64(time.Millisecond))
		if !runAt.After(now) {
			s.log.Info("discarding one-time job (in the past)", "job_id", job.JobID, "task_type", job.TaskType, "run_at", runAt.Format(time.RFC3339))
			_ = s.redisClient.Del(ctx, key).Err()
			discarded++
			continue
		}

		// Check if handler exists for this task type
		_, exists := s.handlers[job.TaskType]
		if !exists {
			s.log.Warn("no handler for restored one-time job, discarding", "job_id", job.JobID, "task_type", job.TaskType)
			_ = s.redisClient.Del(ctx, key).Err()
			discarded++
			continue
		}

		// Check if task type is requestable
		if !requestableTaskTypes[job.TaskType] {
			s.log.Warn("task type is not requestable, discarding", "job_id", job.JobID, "task_type", job.TaskType)
			_ = s.redisClient.Del(ctx, key).Err()
			discarded++
			continue
		}

		// Restore the job
		if err := s.ScheduleOneTimeJob(job.JobID, job.TaskType, runAt, job.Data); err != nil {
			s.log.Error("failed to restore one-time job", "job_id", job.JobID, "task_type", job.TaskType, "error", err)
			_ = s.redisClient.Del(ctx, key).Err()
			discarded++
			continue
		}

		s.log.Info("restored one-time job", "job_id", job.JobID, "task_type", job.TaskType, "run_at", runAt.Format(time.RFC3339))
		restored++
	}

	s.log.Info("one-time jobs restored", "restored", restored, "discarded", discarded)
	return nil
}

// Start begins listening for scheduling requests and starts the scheduler
func (s *TaskScheduler) Start() error {
	// Start the gocron scheduler
	s.scheduler.Start()

	// Set up JetStream consumer for one-time job requests
	if s.jsContext != nil {
		ctx := context.Background()
		subject := natscore.SubjectSchedulerSchedule
		streamName := natscore.StreamScheduler
		consumerName := natscore.ConsumerScheduler

		// Ensure the stream exists
		streamConfigs := []natscore.StreamConfig{
			{
				Name:     streamName,
				Subjects: []string{subject},
				MaxAge:   24 * time.Hour, // Keep messages for 24 hours
			},
		}
		if err := natscore.EnsureStreams(s.jsContext, streamConfigs); err != nil {
			return fmt.Errorf("failed to ensure scheduler stream: %w", err)
		}

		// Get or create the stream
		stream, err := s.jsContext.Stream(ctx, streamName)
		if err != nil {
			return fmt.Errorf("failed to get scheduler stream: %w", err)
		}

		// Create or get durable consumer
		// Use DeliverAllPolicy to get all messages including those published while scheduler was down
		consumerConfig := jetstream.ConsumerConfig{
			Durable:       consumerName,
			DeliverPolicy: jetstream.DeliverAllPolicy,
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       30 * time.Second,
			MaxDeliver:    5,
		}

		consumer, err := natscore.GetOrCreateConsumer(ctx, stream, consumerConfig)
		if err != nil {
			return fmt.Errorf("failed to create scheduler consumer: %w", err)
		}
		s.consumer = consumer

		// Start message processing loop
		go func() {
			for {
				select {
				case <-s.stopChan:
					return
				default:
					// Fetch up to 10 messages at a time
					msgs, err := consumer.Fetch(10, jetstream.FetchMaxWait(5*time.Second))
					if err != nil {
						if err == context.DeadlineExceeded {
							continue
						}
						s.log.Error("failed to fetch scheduler messages", "subject", subject, "error", err)
						time.Sleep(time.Second)
						continue
					}

					for msg := range msgs.Messages() {
						jetstreamMsg := msg
						// Acknowledge message receipt immediately to prevent redelivery while processing
						if err := jetstreamMsg.InProgress(); err != nil {
							s.log.Warn("failed to send InProgress for scheduler message", "subject", subject, "error", err)
						}

						// Process the message
						s.processScheduleRequest(jetstreamMsg)
					}
				}
			}
		}()

		s.log.Info("scheduler started with JetStream consumer", "subject", subject, "consumer", consumerName, "stream", streamName)
	} else {
		s.log.Info("scheduler started (no JetStream context for one-time jobs)")
	}

	return nil
}

// processScheduleRequest processes a schedule request message from JetStream
func (s *TaskScheduler) processScheduleRequest(msg jetstream.Msg) {
	var req natscore.ScheduleRequest
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		s.log.Error("failed to parse schedule request", "error", err)
		if err := msg.Nak(); err != nil {
			s.log.Warn("failed to nack invalid message", "error", err)
		}
		return
	}

	// Generate unique job ID if not provided
	if req.JobID == "" {
		jobID, err := s.generateJobID()
		if err != nil {
			s.log.Error("failed to generate job ID", "error", err)
			if err := msg.Nak(); err != nil {
				s.log.Warn("failed to nack message", "error", err)
			}
			return
		}
		req.JobID = jobID
	}

	s.log.Info("received schedule request", "job_id", req.JobID, "task_type", req.TaskType, "run_at", req.RunAt)

	// Check if run_at is in the future
	runAt := time.Unix(0, req.RunAt*int64(time.Millisecond))
	now := time.Now()
	if !runAt.After(now) {
		s.log.Warn("dropping schedule request - run_at is not in the future",
			"job_id", req.JobID,
			"task_type", req.TaskType,
			"run_at", runAt.Format(time.RFC3339),
			"now", now.Format(time.RFC3339))
		// Acknowledge and drop the message
		if err := msg.Ack(); err != nil {
			s.log.Warn("failed to ack message", "error", err)
		}
		return
	}

	// Check if handler exists
	_, exists := s.handlers[req.TaskType]
	if !exists {
		s.log.Warn("no handler registered for task type", "task_type", req.TaskType)
		if err := msg.Ack(); err != nil {
			s.log.Warn("failed to ack message", "error", err)
		}
		return
	}

	// Schedule the one-time job
	if err := s.ScheduleOneTimeJob(req.JobID, req.TaskType, runAt, req.Data); err != nil {
		s.log.Error("failed to schedule one-time job", "job_id", req.JobID, "task_type", req.TaskType, "error", err)
		if err := msg.Nak(); err != nil {
			s.log.Warn("failed to nack message", "error", err)
		}
		return
	}

	// Acknowledge successful processing
	if err := msg.Ack(); err != nil {
		s.log.Warn("failed to ack message", "error", err)
	}
}

// Stop stops listening for scheduling requests and stops the scheduler
func (s *TaskScheduler) Stop() {
	// Stop the message processing loop
	if s.stopChan != nil {
		close(s.stopChan)
	}

	// Stop the gocron scheduler
	if err := s.scheduler.Shutdown(); err != nil {
		s.log.Warn("error shutting down scheduler", "error", err)
	}

	s.log.Info("scheduler stopped")
}
