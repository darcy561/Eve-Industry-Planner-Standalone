package main

import (
	"context"
	"fmt"
	"time"

	esicore "eve-industry-planner/internal/core/esi"
	natscore "eve-industry-planner/internal/core/nats"
	"eve-industry-planner/internal/shared/logs"
	"eve-industry-planner/internal/tasks/esi"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	antslib "github.com/panjf2000/ants/v2"
	redislib "github.com/redis/go-redis/v9"
)

// TaskFunc is a function that processes a message
type TaskFunc func(natsMessage esi.MessageInterface, redisClient *redislib.Client, natsConn *natslib.Conn, esiClient esicore.ClientInterface)

// SubscriberConfig holds the configuration for a subscriber
type SubscriberConfig struct {
	Subject      string
	ConsumerName string
	StreamName   string
	TaskName     string // For logging purposes (e.g., "system indexes refresh", "adjusted prices refresh")
	TaskFunc     TaskFunc
	ESIClient    esicore.ClientInterface
}

// SubscribeToSubject sets up a JetStream pull consumer for a specific subject.
// Returns a cleanup function and an error if subscription fails.
func SubscribeToSubject(js jetstream.JetStream, redisClient *redislib.Client, pool *antslib.Pool, natsConn *natslib.Conn, config SubscriberConfig) (func(context.Context), error) {
	ctx := context.Background()

	// Get or create the stream
	stream, err := js.Stream(ctx, config.StreamName)
	if err != nil {
		return nil, err
	}

	// Create or get durable consumer
	// Use DeliverLastPolicy to only get new messages, avoiding reprocessing old messages on startup
	// FilterSubject ensures this consumer only receives messages for its specific subject
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       config.ConsumerName,
		FilterSubject: config.Subject,
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
				logs.Debug("fetching messages from NATS", "subject", config.Subject)
				msgs, err := consumer.Fetch(10, jetstream.FetchMaxWait(5*time.Second))
				if err != nil {
					if err == context.DeadlineExceeded {
						logs.Debug("fetch timeout (no messages available)", "subject", config.Subject)
						continue
					}
					logs.Error("failed to fetch messages", "subject", config.Subject, "error", err)
					time.Sleep(time.Second)
					continue
				}

				msgCount := 0
				for msg := range msgs.Messages() {
					msgCount++
					// Capture msg for closure
					jetstreamMsg := msg
					deliveryCount, sequence := getMessageMetadata(jetstreamMsg)
					logs.Info(fmt.Sprintf("received %s message", config.TaskName), "subject", config.Subject, "sequence", sequence, "delivery_count", deliveryCount)

					// Acknowledge message receipt immediately to prevent redelivery while waiting for pool
					if err := jetstreamMsg.InProgress(); err != nil {
						logs.Warn("failed to send InProgress for message", "subject", config.Subject, "sequence", sequence, "error", err)
					}
					// Submit task to goroutine pool - will wait if pool is full
					err := pool.Submit(func() {
						// Recover from panics to ensure message is always acknowledged
						defer func() {
							if r := recover(); r != nil {
								logs.Error(fmt.Sprintf("panic in %s task", config.TaskName), "error", r, "subject", config.Subject, "sequence", sequence, "delivery_count", deliveryCount)
								// Nack the message on panic so it can be retried
								if err := jetstreamMsg.Nak(); err != nil {
									logs.Warn("failed to nack message after panic", "subject", config.Subject, "sequence", sequence, "error", err)
								} else {
									logs.Info("message nacked after panic", "subject", config.Subject, "sequence", sequence)
								}
							}
						}()
						// Pass jetstream.Msg wrapped as MessageInterface
						config.TaskFunc(wrapJetStreamMsg(jetstreamMsg), redisClient, natsConn, config.ESIClient)
					})
					if err != nil {
						logs.Error("failed to submit task to pool", "subject", config.Subject, "sequence", sequence, "error", err)
						// Nack the message if we can't process it
						if err := jetstreamMsg.Nak(); err != nil {
							logs.Warn("failed to nack message", "subject", config.Subject, "sequence", sequence, "error", err)
						} else {
							logs.Info("message nacked due to pool submission failure", "subject", config.Subject, "sequence", sequence)
						}
					}
				}
				if msgCount > 0 {
					logs.Info("fetched batch of messages", "subject", config.Subject, "count", msgCount, "messages_processed", msgCount)
				} else {
					logs.Debug("fetch returned but no messages in batch", "subject", config.Subject)
				}
			}
		}
	}()

	logs.Info(fmt.Sprintf("subscribed to %s", config.TaskName), "subject", config.Subject, "consumer", config.ConsumerName, "type", "pull")

	cleanup := func(ctx context.Context) {
		close(stopChan)
		// Messages channel will be closed, processing will stop
	}

	return cleanup, nil
}
