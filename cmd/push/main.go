package main

import (
	"context"
	"time"

	"eve-industry-planner/internal/core/mongo"
	"eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/shared"
	"eve-industry-planner/internal/shared/logs"
	"eve-industry-planner/internal/ws"
)

func main() {
	// signal-aware context so we can cancel on startup failures
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

	natsConn, err := nats.Connect()
	if err != nil {
		logs.Error("failed to connect to nats", "err", err)
		cancel()
		shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
		return
	}
	cleanupFns = append(cleanupFns, func(c context.Context) { nats.Cleanup(natsConn) })

	_ = ws.StartServer(":8090")
	logs.Info("push service running")

	// normal blocking shutdown
	shared.WaitForShutdown(ctx, 5*time.Second, cleanupFns...)
}
