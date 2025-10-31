package main

import (
	"context"
	"time"

	"eve-industry-planner/internal/core/mongo"
	natscore "eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/core/redis"
	"eve-industry-planner/internal/shared"
	"eve-industry-planner/internal/shared/logs"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	antslib "github.com/panjf2000/ants/v2"
	redislib "github.com/redis/go-redis/v9"
)

func main() {
	// signal-aware context first
	ctx, cancel := shared.NewSignalContext(context.Background())

	cleanupFns := []func(context.Context){}

	mongoClient, err := mongo.Connect()
	if err != nil {
		logs.Error("failed to connect to mongo", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { mongo.Cleanup(c, mongoClient) })

	natsConn, js, err := natscore.ConnectJetStream()
	if err != nil {
		logs.Error("failed to connect to nats jetstream", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { natscore.Cleanup(natsConn) })

	// Ensure JetStream streams exist
	streams := []natscore.StreamConfig{
		{
			Name:     natscore.StreamESIRefresh,
			Subjects: []string{natscore.SubjectRefreshSystemIndexes, natscore.SubjectRefreshAdjustedPrices},
			MaxAge:   24 * time.Hour,
		},
	}
	if err := natscore.EnsureStreams(js, streams); err != nil {
		logs.Error("failed to ensure JetStream streams", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}

	redisClient, err := redis.Connect()
	if err != nil {
		logs.Error("failed to connect to redis", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { redis.Cleanup(c, redisClient) })

	// Create goroutine pool for distributing tasks
	// Pool size: 100 workers (can be adjusted based on load)
	// Using blocking mode so tasks wait for pool availability
	poolSize := 10
	pool, err := antslib.NewPool(poolSize, antslib.WithNonblocking(false))
	if err != nil {
		logs.Error("failed to create goroutine pool", "error", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) {
		// Release pool with 5 second timeout
		pool.ReleaseTimeout(5 * time.Second)
	})
	logs.Info("goroutine pool created", "size", poolSize)

	logs.Info("worker service running")

	// Setup all subscribers
	subscribers := []struct {
		name    string
		setupFn func(jetstream.JetStream, *redislib.Client, *antslib.Pool, *natslib.Conn) (func(context.Context), error)
	}{
		{"systemIndexes", SubscribeSystemIndexes},
		{"adjustedPrices", SubscribeAdjustedPrices},
	}

	// Initialize all subscribers on startup
	for _, sub := range subscribers {
		cleanup, err := sub.setupFn(js, redisClient, pool, natsConn)
		if err != nil {
			logs.Error("failed to setup subscriber", "subscriber", sub.name, "error", err)
			cancel()
			shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
			return
		}
		cleanupFns = append(cleanupFns, cleanup)
	}

	// normal blocking shutdown
	shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
}
