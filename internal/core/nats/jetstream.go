package nats

import (
	"context"
	"fmt"
	"time"

	"eve-industry-planner/internal/shared/logs"

	"github.com/nats-io/nats.go/jetstream"
)

// EnsureStreams creates JetStream streams for the given subjects if they don't exist.
func EnsureStreams(js jetstream.JetStream, streams []StreamConfig) error {
	ctx := context.Background()
	for _, streamConfig := range streams {
		stream, err := js.Stream(ctx, streamConfig.Name)
		if err == nil && stream != nil {
			// Stream already exists
			logs.Info("stream already exists", "name", streamConfig.Name)
			continue
		}

		cfg := jetstream.StreamConfig{
			Name:      streamConfig.Name,
			Subjects:  streamConfig.Subjects,
			Retention: jetstream.LimitsPolicy,
			Storage:   jetstream.FileStorage,
			MaxAge:    streamConfig.MaxAge,
		}

		_, err = js.CreateStream(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create stream %s: %w", streamConfig.Name, err)
		}
		logs.Info("created JetStream stream", "name", streamConfig.Name, "subjects", streamConfig.Subjects)
	}
	return nil
}

// StreamConfig represents configuration for a JetStream stream
type StreamConfig struct {
	Name     string
	Subjects []string
	MaxAge   time.Duration
}

// GetOrCreateConsumer gets an existing consumer or creates a new one with the specified config.
// If a consumer exists with a different DeliverPolicy, it will be deleted and recreated
// since DeliverPolicy is immutable on existing consumers.
func GetOrCreateConsumer(ctx context.Context, stream jetstream.Stream, consumerConfig jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	// Try to get existing consumer
	existingConsumer, err := stream.Consumer(ctx, consumerConfig.Durable)
	if err == nil {
		// Consumer exists, check if DeliverPolicy matches (it's immutable, so if it doesn't match, we need to delete and recreate)
		info := existingConsumer.CachedInfo()
		if info == nil || info.Config.DeliverPolicy != consumerConfig.DeliverPolicy {
			// Delete the existing consumer to recreate with correct policy
			if err := stream.DeleteConsumer(ctx, consumerConfig.Durable); err != nil {
				logs.Warn("failed to delete existing consumer with different policy", "consumer", consumerConfig.Durable, "error", err)
			}
		} else {
			// Consumer exists with correct policy, use it
			return existingConsumer, nil
		}
	}

	// Create new consumer
	consumer, err := stream.CreateConsumer(ctx, consumerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer %s: %w", consumerConfig.Durable, err)
	}

	return consumer, nil
}
