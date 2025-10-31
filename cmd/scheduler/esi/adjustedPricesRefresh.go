package esi

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	natscore "eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/scheduler"
	taskscore "eve-industry-planner/internal/tasks"

	"github.com/nats-io/nats.go/jetstream"
	redislib "github.com/redis/go-redis/v9"
)

// ScheduleAdjustedPricesRefresh sets up the task handler for adjusted prices refresh.
// Returns a cleanup function and an error if scheduling fails.
func ScheduleAdjustedPricesRefresh(deps scheduler.Dependencies, sched scheduler.Scheduler) (func(), error) {
	jsContext := deps.JSContext
	redisClient := deps.Redis
	log := deps.Log

	// Register the task handler
	sched.RegisterHandler(taskscore.TaskTypeRefreshAdjustedPrices, func(ctx context.Context, data json.RawMessage) error {
		// data is optional - for refreshAdjustedPrices we don't need it, but the interface requires it
		return triggerAdjustedPricesRefresh(jsContext, log, redisClient, ctx)
	})

	// Return a function that will run the startup check AFTER RestoreJobs completes
	// This will be called by the registry after restoration
	startupCheck := func() {
		ctx := context.Background()
		taskType := taskscore.TaskTypeRefreshAdjustedPrices

		// First check if there's already a scheduled job for this task type (restored from Redis)
		if sched.HasScheduledJob(taskType) {
			log.Info("scheduled job already exists, skipping startup check")
			return
		}

		nextRefresh, err := getNextAdjustedPricesRefreshTime(redisClient, ctx)
		now := time.Now()

		if err != nil {
			// No data exists, trigger immediately
			log.Info("no existing data found, triggering initial adjusted prices refresh")
			triggerAdjustedPricesRefresh(jsContext, log, redisClient, ctx)
			return
		}

		if !nextRefresh.After(now) {
			// Data is stale, trigger refresh
			log.Info("data is stale, triggering adjusted prices refresh",
				"next_refresh", nextRefresh.Format(time.RFC3339),
				"now", now.Format(time.RFC3339))
			triggerAdjustedPricesRefresh(jsContext, log, redisClient, ctx)
			return
		}

		// Data is fresh - no need to schedule, worker will schedule next refresh when it runs
		log.Info("data is fresh, waiting for worker to schedule next refresh",
			"next_refresh", nextRefresh.Format(time.RFC3339),
			"fresh_for", time.Until(nextRefresh).String())
	}

	// Return cleanup function and startup check function
	// The registry will call startupCheck after RestoreJobs completes
	return func() {
		startupCheck()
	}, nil
}

// getNextAdjustedPricesRefreshTime reads the next_refresh timestamp from Redis and returns a time.Time
func getNextAdjustedPricesRefreshTime(redisClient *redislib.Client, ctx context.Context) (time.Time, error) {
	s, err := redisClient.Get(ctx, "esi:market_prices:next_refresh").Result()
	if err != nil {
		return time.Time{}, err
	}
	millis, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, millis*int64(time.Millisecond)), nil
}

// triggerAdjustedPricesRefresh publishes a JetStream message to trigger adjusted prices refresh
func triggerAdjustedPricesRefresh(js jetstream.JetStream, log *slog.Logger, redisClient *redislib.Client, ctx context.Context) error {
	subject := natscore.SubjectRefreshAdjustedPrices

	// Publish to JetStream stream
	_, err := js.Publish(ctx, subject, []byte(""))
	if err != nil {
		log.Error("failed to publish adjusted prices refresh trigger", "error", err)
		return err
	}
	log.Info("adjusted prices refresh triggered", "subject", subject)
	return nil
}
