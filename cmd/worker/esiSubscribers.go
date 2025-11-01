package main

import (
	"context"

	esicore "eve-industry-planner/internal/core/esi"
	natscore "eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/tasks/esi"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	antslib "github.com/panjf2000/ants/v2"
	redislib "github.com/redis/go-redis/v9"
)

// SubscribeSystemIndexes sets up the JetStream pull consumer for system indexes refresh messages.
// Returns a cleanup function and an error if subscription fails.
func SubscribeSystemIndexes(js jetstream.JetStream, redisClient *redislib.Client, pool *antslib.Pool, natsConn *natslib.Conn, esiClient esicore.ClientInterface) (func(context.Context), error) {
	return SubscribeToSubject(js, redisClient, pool, natsConn, SubscriberConfig{
		Subject:      natscore.SubjectRefreshSystemIndexes,
		ConsumerName: natscore.ConsumerWorkerSystemIndexes,
		StreamName:   natscore.StreamESIRefresh,
		TaskName:     natscore.TaskNameSystemIndexesRefresh,
		TaskFunc:     esi.RefreshSystemIndexes,
		ESIClient:    esiClient,
	})
}

// SubscribeAdjustedPrices sets up the JetStream pull consumer for adjusted prices refresh messages.
// Returns a cleanup function and an error if subscription fails.
func SubscribeAdjustedPrices(js jetstream.JetStream, redisClient *redislib.Client, pool *antslib.Pool, natsConn *natslib.Conn, esiClient esicore.ClientInterface) (func(context.Context), error) {
	return SubscribeToSubject(js, redisClient, pool, natsConn, SubscriberConfig{
		Subject:      natscore.SubjectRefreshAdjustedPrices,
		ConsumerName: natscore.ConsumerWorkerAdjustedPrices,
		StreamName:   natscore.StreamESIRefresh,
		TaskName:     natscore.TaskNameAdjustedPricesRefresh,
		TaskFunc:     esi.RefreshAdjustedPrices,
		ESIClient:    esiClient,
	})
}

