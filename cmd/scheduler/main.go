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
	// Core dependencies commonly needed by jobs
	mongoClient, err := mongo.Connect()
	if err != nil {
		logs.Error("failed to connect to mongo", "err", err)
		return
	}
	natsConn, err := nats.Connect()
	if err != nil {
		logs.Error("failed to connect to nats", "err", err)
		return
	}
	redisClient, err := redis.Connect()
	if err != nil {
		logs.Error("failed to connect to redis", "err", err)
		return
	}

	logs.Info("scheduler service running")

	// graceful shutdown via shared helper
	ctx, _ := shared.NewSignalContext(context.Background())

	// Start lightweight scheduler (tickers)
	stop := startScheduler("scheduler")

	shared.WaitForShutdown(ctx, 5*time.Second,
		func(cctx context.Context) { stop() },
		func(cctx context.Context) { nats.Cleanup(natsConn) },
		func(cctx context.Context) { mongo.Cleanup(cctx, mongoClient) },
		func(cctx context.Context) { redis.Cleanup(cctx, redisClient) },
	)
}
