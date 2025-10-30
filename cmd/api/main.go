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

// testLogging emits sample logs at all levels to validate the stack, every 10s until ctx done.
func testLogging(ctx context.Context) {
	log := logs.Component("api")

	emit := func() {
		log.Debug("test debug log", "example", true, "number", 42)
		log.Info("test info log", "status", "ok")
		log.Warn("test warn log", "latency_ms", 123)
		log.Error("test error log", "error", "sample")
	}

	emit()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			emit()
		}
	}
}

func main() {
	redisClient, err := redis.Connect()
	if err != nil {
		logs.Error("failed to connect to redis", "err", err)
		return
	}
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

	logs.Info("api service running")

	// graceful shutdown via shared helper
	ctx, _ := shared.NewSignalContext(context.Background())

	// Emit sample logs so you can verify in Grafana/Loki, loop every 10s
	go testLogging(ctx)
	shared.WaitForShutdown(ctx, 5*time.Second,
		func(c context.Context) { nats.Cleanup(natsConn) },
		func(c context.Context) { mongo.Cleanup(c, mongoClient) },
		func(c context.Context) { redis.Cleanup(c, redisClient) },
	)
}
