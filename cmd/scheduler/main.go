package main

import (
	"context"
	"time"

	"eve-industry-planner/internal/core/mongo"
	"eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/core/redis"
	"eve-industry-planner/internal/shared"
	"eve-industry-planner/internal/shared/logs"

	esischedule "eve-industry-planner/cmd/scheduler/esi"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	redislib "github.com/redis/go-redis/v9"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

// startScheduler runs periodic jobs using standard library tickers. Returns a stop function.
func startScheduler(logComponent string, natsConn *natslib.Conn, jsContext jetstream.JetStream, redisClient *redislib.Client, mongoClient *mongodriver.Client) func() {
	log := logs.Component(logComponent)
	stop := make(chan struct{})

	// Create job registry to manage all schedulers
	registry := NewJobRegistry(log)

	// Register all schedulers
	registry.Register(esischedule.ScheduleIndustrySystemsRefresh)
	registry.Register(esischedule.ScheduleAdjustedPricesRefresh)
	// Add more schedulers here:
	// registry.Register(market.ScheduleMarketHistoryRefresh)

	// Start all registered schedulers
	if err := registry.Start(natsConn, jsContext, redisClient, mongoClient); err != nil {
		log.Error("failed to start job registry", "error", err)
		return func() {
			registry.Stop()
			close(stop)
		}
	}

	return func() {
		registry.Stop()
		close(stop)
	}
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

	natsConn, jsContext, err := nats.ConnectJetStream()
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
	stop := startScheduler("scheduler", natsConn, jsContext, redisClient, mongoClient)
	cleanupFns = append(cleanupFns, func(c context.Context) { stop() })

	// normal blocking shutdown
	shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
}
