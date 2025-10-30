package redis

import (
	"context"
	"errors"
	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

func Connect() (*redis.Client, error) {
	cfg := config.LoadConfig()

	retryCount, err := strconv.Atoi(cfg.RedisRetryCount)
	if err != nil {
		retryCount = 5
	}

	retryDelay, err := strconv.Atoi(cfg.RedisRetryDelay)
	if err != nil {
		retryDelay = 5
	}

	for i := 0; i < retryCount; i++ {
		client := redis.NewClient(&redis.Options{
			Addr: cfg.RedisURL,
		})

		err = client.Ping(context.Background()).Err()
		if err == nil {
			i++
			message := fmt.Sprintf("Connected to Redis on attempt %d/%d", i, retryCount)
			logs.Info(message)
			return client, nil
		}
		i++
		message := fmt.Sprintf("Failed to connect to Redis. Attempt %d/%d. Error: %v", i, retryCount, err)
		logs.Error(message)
		time.Sleep(time.Duration(retryDelay) * time.Second)
	}
	message := fmt.Sprintf("Failed to connect to Redis after %d attempts. Exiting...", retryCount)
	logs.Error(message)
	return nil, errors.New(message)
}

func Cleanup(ctx context.Context, client *redis.Client) {
	if client == nil {
		return
	}
	_ = client.Close()
}
