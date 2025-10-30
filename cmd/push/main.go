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
	_ = ws.StartServer(":8090")
	logs.Info("push service running")

	// graceful shutdown via shared helper
	ctx, _ := shared.NewSignalContext(context.Background())
	shared.WaitForShutdown(ctx, 5*time.Second,
		func(c context.Context) { nats.Cleanup(natsConn) },
		func(c context.Context) { mongo.Cleanup(c, mongoClient) },
	)
}
