package esi

import (
	"context"
	"encoding/json"
	"time"

	natscore "eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/scheduler"
	taskscore "eve-industry-planner/internal/tasks"
)

// ScheduleAdjustedPricesRefresh sets up a static cron job for adjusted prices refresh (hourly).
// Returns a cleanup function and an error if scheduling fails.
func ScheduleAdjustedPricesRefresh(deps scheduler.Dependencies, sched scheduler.Scheduler) (func(), error) {
	jsContext := deps.JSContext
	log := deps.Log

	// Register the task handler
	sched.RegisterHandler(taskscore.TaskTypeRefreshAdjustedPrices, func(ctx context.Context, data json.RawMessage) error {
		// Just publish to JetStream - the worker will handle the actual refresh
		subject := natscore.SubjectRefreshAdjustedPrices
		log.Info("publishing adjusted prices refresh trigger", "subject", subject)

		// Use standard EmptyMessage helper for simple trigger messages
		if err := scheduler.PublishEmptyMessage(jsContext, subject); err != nil {
			log.Error("failed to publish adjusted prices refresh trigger", "subject", subject, "error", err)
			return err
		}

		log.Info("adjusted prices refresh triggered", "subject", subject, "timestamp", time.Now().UnixNano())
		return nil
	})

	return func() {}, nil
}
