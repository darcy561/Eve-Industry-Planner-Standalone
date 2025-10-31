package main

import (
	"context"
	"time"

	natscore "eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/shared/logs"
	"eve-industry-planner/internal/tasks/esi"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	antslib "github.com/panjf2000/ants/v2"
	redislib "github.com/redis/go-redis/v9"
)

// SubscribeSystemIndexes sets up the JetStream pull consumer for system indexes refresh messages.
// Returns a cleanup function and an error if subscription fails.
func SubscribeSystemIndexes(js jetstream.JetStream, redisClient *redislib.Client, pool *antslib.Pool, natsConn *natslib.Conn) (func(context.Context), error) {
	subject := natscore.SubjectRefreshSystemIndexes
	consumerName := natscore.ConsumerWorkerSystemIndexes
	streamName := natscore.StreamESIRefresh

	ctx := context.Background()

	// Get or create the stream
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return nil, err
	}

	// Create or get durable consumer
	// Use DeliverLastPolicy to only get new messages, avoiding reprocessing old messages on startup
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       consumerName,
		DeliverPolicy: jetstream.DeliverLastPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    5,
	}

	consumer, err := natscore.GetOrCreateConsumer(ctx, stream, consumerConfig)
	if err != nil {
		return nil, err
	}

	// Start message processing loop
	stopChan := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopChan:
				return
			default:
				// Fetch up to 10 messages at a time
				msgs, err := consumer.Fetch(10, jetstream.FetchMaxWait(5*time.Second))
				if err != nil {
					if err == context.DeadlineExceeded {
						continue
					}
					logs.Error("failed to fetch messages", "subject", subject, "error", err)
					time.Sleep(time.Second)
					continue
				}

				for msg := range msgs.Messages() {
					// Capture msg for closure
					jetstreamMsg := msg
					// Acknowledge message receipt immediately to prevent redelivery while waiting for pool
					if err := jetstreamMsg.InProgress(); err != nil {
						logs.Warn("failed to send InProgress for message", "subject", subject, "error", err)
					}
					// Submit task to goroutine pool - will wait if pool is full
					err := pool.Submit(func() {
						// Pass jetstream.Msg wrapped as MessageInterface
						esi.RefreshSystemIndexes(wrapJetStreamMsg(jetstreamMsg), redisClient, natsConn)
					})
					if err != nil {
						logs.Error("failed to submit task to pool", "subject", subject, "error", err)
						// Nack the message if we can't process it
						if err := jetstreamMsg.Nak(); err != nil {
							logs.Warn("failed to nack message", "subject", subject, "error", err)
						}
					}
				}
			}
		}
	}()

	logs.Info("subscribed to industry systems refresh", "subject", subject, "consumer", consumerName, "type", "pull")

	cleanup := func(ctx context.Context) {
		close(stopChan)
		// Messages channel will be closed, processing will stop
	}

	return cleanup, nil
}
