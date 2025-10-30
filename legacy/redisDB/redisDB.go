package redisDB

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"

	"eveindustryplanner.com/esiworkers/logger"
	"github.com/mitchellh/mapstructure"
	"github.com/redis/go-redis/v9"
)

type SortedSetResult struct {
	TypeID      string
	LastUpdated time.Time
}

var client *redis.Client

func Connect() {
	log := logger.GetLogger(context.Background(), logger.Redis)
	retryCount, err := strconv.Atoi(os.Getenv("REDIS_RETRY_COUNT"))
	if err != nil || retryCount <= 0 {
		retryCount = 5
	}

	retryDelay, err := strconv.Atoi(os.Getenv("REDIS_RETRY_DELAY"))
	if err != nil || retryDelay <= 0 {
		retryDelay = 5
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis:6379"
	}

	for i := 0; i < retryCount; i++ {
		client = redis.NewClient(&redis.Options{
			Addr: redisURL,
		})

		err = client.Ping(context.Background()).Err()
		if err == nil {
			i++
			message := fmt.Sprintf("Connected to Redis on attempt %d/%d", i, retryCount)
			log.Info(message)
			return
		}

		i++
		message := fmt.Sprintf("Failed to connect to Redis. Attempt %d/%d. Error: %v", i, retryCount, err)
		log.Error(message)
		time.Sleep(time.Duration(retryDelay) * time.Second)
	}

	message := fmt.Sprintf("Failed to connect to Redis after %d attempts. Exiting...", retryCount)
	log.Error(message)
}

func SaveETagsAsHash(ctx context.Context, key string, m map[int]string) error {

	redisMap := make(map[string]any)
	for k, v := range m {
		redisMap[fmt.Sprintf("%v", k)] = v
	}

	err := client.HSet(ctx, key, redisMap).Err()
	if err != nil {
		return fmt.Errorf("could not store user data: %v", err)
	}

	return nil
}

func GetETagsAsMap(ctx context.Context, key string) (m map[int]string, err error) {

	m = make(map[int]string)

	r, err := client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get hash from Redis: %w", err)
	}

	for k, v := range r {
		pageNumber, err := strconv.Atoi(k)
		if err != nil {
			fmt.Println("Error: Failed to convert page number:", err)
			continue
		}
		m[pageNumber] = v
	}
	return m, nil
}

func SaveStructAsHash(ctx context.Context, key string, data any) error {
	if reflect.ValueOf(data).Kind() != reflect.Struct {
		return fmt.Errorf("input data must be a struct, got %T", data)
	}

	var redisMap map[string]any

	err := mapstructure.Decode(data, &redisMap)
	if err != nil {
		return fmt.Errorf("could not convert struct to map: %v", err)
	}
	err = client.HSet(ctx, key, redisMap).Err()
	if err != nil {
		return fmt.Errorf("could not store user data: %v", err)
	}

	return nil
}

func GetRedisMap(ctx context.Context, key string) (map[string]string, error) {
	data, err := client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("no data found for key: %s", key)
	}

	return data, nil
}

func SaveToSortedSet(ctx context.Context, setKey string, member string, score float64) (err error) {
	_, err = client.ZAdd(ctx, setKey, redis.Z{Member: member, Score: score}).Result()

	if err != nil {
		fmt.Println("Error saving to sorted set:", err)
		return
	}
	return
}

func GetScoreFromSortedSet(ctx context.Context, setKey string, member string) (score float64, err error) {
	score, err = client.ZScore(ctx, setKey, member).Result()

	if err != nil {
		if err == redis.Nil {
			err = fmt.Errorf("member not found in the sorted set")
		} else {
			err = fmt.Errorf("error retrieving score from sorted set: %v ", err)
		}
		return 0, err
	}

	return score, nil
}

func SaveAsJSON[T any](ctx context.Context, key string, value T) error {
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal struct to JSON: %v", err)
	}

	err = client.Set(ctx, key, valueJSON, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to save struct to Redis: %v", err)
	}

	return nil
}

func RetrieveOldestValuesFromSortedSet(ctx context.Context, setKey string, numberOfResults int) ([]SortedSetResult, error) {
	if numberOfResults <= 0 {
		return nil, fmt.Errorf("numberOfResults must be greater than 0")
	}
	scores, err := client.ZRangeWithScores(ctx, setKey, 0, int64(numberOfResults-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("error retrieving from sorted set: %w", err)
	}

	results := make([]SortedSetResult, len(scores))

	for i, score := range scores {

		memberStr, ok := score.Member.(string)
		if !ok {
			return nil, fmt.Errorf("member is not a string")
		}

		results[i] = SortedSetResult{
			TypeID:      memberStr,
			LastUpdated: time.UnixMilli(int64(score.Score)),
		}
	}

	return results, nil
}

func GetKeyAsInt(ctx context.Context, key string) (int, error) {
	result, err := client.Get(ctx, key).Int()

	if err != nil {
		return 0, err
	}
	return result, nil
}

func IncrementRedisValue(ctx context.Context, key string) {
	client.Incr(ctx, key)
}

func DecrementRedisValue(ctx context.Context, key string) {
	client.Decr(ctx, key)
}

func SetKeyExpirey(ctx context.Context, key string, exp time.Duration) {
	client.Expire(ctx, key, exp)
}

func GetValue(ctx context.Context, key string) (string, error) {
	return client.Get(ctx, key).Result()
}
