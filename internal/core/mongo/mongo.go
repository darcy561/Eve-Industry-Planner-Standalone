package mongo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ConnectMongo establishes a client and returns it.
func Connect() (*mongo.Client, error) {
	cfg := config.LoadConfig()

	retryCount, err := strconv.Atoi(cfg.MongoRetryCount)
	if err != nil {
		retryCount = 5
	}

	retryDelay, err := strconv.Atoi(cfg.MongoRetryDelay)
	if err != nil {
		retryDelay = 5
	}

	for i := 0; i < retryCount; i++ {
		client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(cfg.MongoURI))
		if err == nil {
			i++
			message := fmt.Sprintf("Connected to Mongo on attempt %d/%d", i, cfg.MongoRetryCount)
			logs.Info(message)
			return client, nil
		}
		i++
		message := fmt.Sprintf("Failed to connect to Mongo. Attempt %d/%d. Error: %v", i, cfg.MongoRetryCount, err)
		logs.Error(message)
		time.Sleep(time.Duration(retryDelay) * time.Second)
	}

	message := fmt.Sprintf("Failed to connect to Mongo after %d attempts. Exiting...", retryCount)
	logs.Error(message)
	return nil, errors.New(message)
}

func Cleanup(ctx context.Context, client *mongo.Client) {
	if client == nil {
		return
	}
	_ = client.Disconnect(ctx)
}
