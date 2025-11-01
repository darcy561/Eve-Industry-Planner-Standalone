package main

import (
	"log/slog"

	"eve-industry-planner/internal/scheduler"
	taskscore "eve-industry-planner/internal/tasks"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	redislib "github.com/redis/go-redis/v9"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

// SchedulerFunc represents a function that sets up a scheduled job.
// Accepts a Dependencies struct containing all available dependencies and a Scheduler interface.
// Returns a cleanup function and an error if scheduling fails.
type SchedulerFunc func(scheduler.Dependencies, scheduler.Scheduler) (func(), error)

// JobRegistry manages all scheduled jobs
type JobRegistry struct {
	log              *slog.Logger
	cleanups         []func()
	schedulers       []SchedulerFunc
	schedulerHandler *TaskScheduler
}

// NewJobRegistry creates a new job registry
func NewJobRegistry(log *slog.Logger) *JobRegistry {
	return &JobRegistry{
		log:        log,
		cleanups:   []func(){},
		schedulers: []SchedulerFunc{},
	}
}

// Register adds a scheduler function to the registry
func (r *JobRegistry) Register(scheduler SchedulerFunc) {
	r.schedulers = append(r.schedulers, scheduler)
}

// Start registers all schedulers
func (r *JobRegistry) Start(natsConn *natslib.Conn, jsContext jetstream.JetStream, redisClient *redislib.Client, mongoClient *mongodriver.Client) error {
	// Create the task scheduler
	var err error
	r.schedulerHandler, err = NewTaskScheduler(jsContext, redisClient, natsConn, r.log)
	if err != nil {
		return err
	}

	deps := scheduler.Dependencies{
		NATS:      natsConn,
		JSContext: jsContext,
		Redis:     redisClient,
		Mongo:     mongoClient,
		Log:       r.log,
	}

	// Register handlers first
	for _, schedulerFunc := range r.schedulers {
		cleanup, err := schedulerFunc(deps, r.schedulerHandler)
		if err != nil {
			r.log.Error("failed to register scheduler", "error", err)
			// Continue with other schedulers even if one fails
			continue
		}
		r.cleanups = append(r.cleanups, cleanup)
	}

	// Schedule static cron jobs for periodic tasks
	// System indexes: every 5 minutes
	if err := r.schedulerHandler.ScheduleCronJob("*/5 * * * *", taskscore.TaskTypeRefreshSystemIndexes); err != nil {
		r.log.Warn("failed to schedule system indexes cron job", "error", err)
	}

	// Adjusted prices: every 5 minutes
	if err := r.schedulerHandler.ScheduleCronJob("*/5 * * * *", taskscore.TaskTypeRefreshAdjustedPrices); err != nil {
		r.log.Warn("failed to schedule adjusted prices cron job", "error", err)
	}

	// Restore one-time jobs from Redis (after handlers are registered)
	if err := r.schedulerHandler.RestoreOneTimeJobs(); err != nil {
		r.log.Warn("failed to restore one-time jobs from Redis", "error", err)
	}

	// Start the scheduler after all handlers are registered, cron jobs are scheduled, and one-time jobs are restored
	if err := r.schedulerHandler.Start(); err != nil {
		return err
	}

	r.log.Info("job registry started", "schedulers", len(r.cleanups))
	return nil
}

// Stop stops all schedulers and cleans up
func (r *JobRegistry) Stop() {
	// Note: cleanup functions are now used for startup checks, not cleanup
	// There's nothing to clean up here since startup checks were already run
	if r.schedulerHandler != nil {
		r.schedulerHandler.Stop()
	}
	r.log.Info("job registry stopped")
}
