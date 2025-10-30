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

func main() {
	// create signal-aware context first so we can cancel on startup failures
	ctx, cancel := shared.NewSignalContext(context.Background())

	cleanupFns := []func(context.Context){}

	redisClient, err := redis.Connect()
	if err != nil {
		logs.Error("failed to connect to redis", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { redis.Cleanup(c, redisClient) })

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

	logs.Info("api service running")

	go func() {
		if err := StartAPIServer(redisClient, mongoClient, natsConn); err != nil {
			logs.Error("failed to start api server", "err", err)
			cancel()
			shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
			return
		}
	}()

	go func() {
		if err := StartWSServer(redisClient, mongoClient, natsConn); err != nil {
			logs.Error("failed to start websocket server", "err", err)
		}
	}()

	// normal blocking shutdown path
	shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
}
