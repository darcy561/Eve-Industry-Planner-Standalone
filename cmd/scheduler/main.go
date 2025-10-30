package main

import (
	"context"
	"time"

	"eve-industry-planner/internal/core/mongo"
	"eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/core/redis"
	"eve-industry-planner/internal/shared"
	"eve-industry-planner/internal/shared/logs"
)

// startScheduler runs periodic jobs using standard library tickers. Returns a stop function.
func startScheduler(logComponent string) func() {
	log := logs.Component(logComponent)
	stop := make(chan struct{})

	// Heartbeat every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				log.Info("scheduler heartbeat", "ts", time.Now().UTC().Format(time.RFC3339))
			case <-stop:
				return
			}
		}
	}()

	// Add more tickers for other periodic jobs as needed

	return func() { close(stop) }
}

func main() {
	// signal-aware context first
	ctx, cancel := shared.NewSignalContext(context.Background())

	cleanupFns := []func(context.Context){}

	// Core dependencies commonly needed by jobs
	mongoClient, err := mongo.Connect()
	if err != nil {
		logs.Error("failed to connect to mongo", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { mongo.Cleanup(c, mongoClient) })

	natsConn, err := nats.Connect()
	if err != nil {
		logs.Error("failed to connect to nats", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { nats.Cleanup(natsConn) })

	redisClient, err := redis.Connect()
	if err != nil {
		logs.Error("failed to connect to redis", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { redis.Cleanup(c, redisClient) })

	logs.Info("scheduler service running")

	// Start lightweight scheduler (tickers)
	stop := startScheduler("scheduler")
	cleanupFns = append(cleanupFns, func(c context.Context) { stop() })

	// normal blocking shutdown
	shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
}
