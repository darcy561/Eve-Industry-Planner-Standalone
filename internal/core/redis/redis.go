package redis

import (
	"context"
	"encoding/json"
	"errors"
	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

func Connect() (*redis.Client, error) {
	cfg := config.LoadConfig()

	retryCount := 5
	retryDelay := 5 * time.Second

	for i := 0; i < retryCount; i++ {
		client := redis.NewClient(&redis.Options{
			Addr: cfg.REDIS_URL,
		})

		err := client.Ping(context.Background()).Err()
		if err == nil {
			i++
			message := fmt.Sprintf("Connected to Redis on attempt %d/%d", i, retryCount)
			logs.Info(message)
			return client, nil
		}
		i++
		message := fmt.Sprintf("Failed to connect to Redis. Attempt %d/%d. Error: %v", i, retryCount, err)
		logs.Error(message)
		time.Sleep(retryDelay)
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

// SaveJSON stores any JSON-serializable value at the provided key.
func SaveJSON(ctx context.Context, client *redis.Client, key string, value any, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return client.Set(ctx, key, b, ttl).Err()
}

// SetString stores a string value at the provided key.
func SetString(ctx context.Context, client *redis.Client, key, value string, ttl time.Duration) error {
	return client.Set(ctx, key, value, ttl).Err()
}

// GetString retrieves a string value from the provided key.
func GetString(ctx context.Context, client *redis.Client, key string) (string, error) {
	return client.Get(ctx, key).Result()
}

// SaveIndustrySystemIndex stores one industry system index JSON under a namespaced key.
func SaveIndustrySystemIndex(ctx context.Context, client *redis.Client, solarSystemID int32, value any) error {
	key := "esi:industry_systems:" + url.PathEscape(strconv.FormatInt(int64(solarSystemID), 10))
	return SaveJSON(ctx, client, key, value, 0)
}

// SaveIndustrySystemsETag stores the ETag for industry systems.
func SaveIndustrySystemsETag(ctx context.Context, client *redis.Client, etag string) error {
	if etag == "" {
		return nil
	}
	return SetString(ctx, client, "esi:industry_systems:etag", etag, 0)
}

// GetIndustrySystemsETag retrieves the stored ETag, if present.
func GetIndustrySystemsETag(ctx context.Context, client *redis.Client) (string, error) {
	return GetString(ctx, client, "esi:industry_systems:etag")
}

// SaveIndustrySystemsLastUpdated stores the last successful refresh timestamp (millis since epoch).
func SaveIndustrySystemsLastUpdated(ctx context.Context, client *redis.Client, unixMillis int64) error {
	return SetString(ctx, client, "esi:industry_systems:last_updated", strconv.FormatInt(unixMillis, 10), 0)
}

// GetIndustrySystemsNextRefresh retrieves the next refresh timestamp (millis since epoch).
// Returns 0 if not found or on error.
func GetIndustrySystemsNextRefresh(ctx context.Context, client *redis.Client) (int64, error) {
	s, err := GetString(ctx, client, "esi:industry_systems:next_refresh")
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// GetJSON retrieves a JSON value from the provided key and unmarshals it into the target.
func GetJSON(ctx context.Context, client *redis.Client, key string, target any) error {
	val, err := client.Get(ctx, key).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(val), target)
}

// GetIndustrySystemIndex retrieves one industry system index JSON from a namespaced key.
func GetIndustrySystemIndex(ctx context.Context, client *redis.Client, solarSystemID int32, target any) error {
	key := "esi:industry_systems:" + url.PathEscape(strconv.FormatInt(int64(solarSystemID), 10))
	return GetJSON(ctx, client, key, target)
}

// SaveMarketPrice stores one market price JSON under a namespaced key.
func SaveMarketPrice(ctx context.Context, client *redis.Client, typeID int32, value any) error {
	key := "esi:market_prices:" + url.PathEscape(strconv.FormatInt(int64(typeID), 10))
	return SaveJSON(ctx, client, key, value, 0)
}

// SaveMarketPricesETag stores the ETag for market prices.
func SaveMarketPricesETag(ctx context.Context, client *redis.Client, etag string) error {
	if etag == "" {
		return nil
	}
	return SetString(ctx, client, "esi:market_prices:etag", etag, 0)
}

// GetMarketPricesETag retrieves the stored ETag, if present.
func GetMarketPricesETag(ctx context.Context, client *redis.Client) (string, error) {
	return GetString(ctx, client, "esi:market_prices:etag")
}

// SaveMarketPricesLastUpdated stores the last successful refresh timestamp (millis since epoch).
func SaveMarketPricesLastUpdated(ctx context.Context, client *redis.Client, unixMillis int64) error {
	return SetString(ctx, client, "esi:market_prices:last_updated", strconv.FormatInt(unixMillis, 10), 0)
}

// GetMarketPricesNextRefresh retrieves the next refresh timestamp (millis since epoch).
// Returns 0 if not found or on error.
func GetMarketPricesNextRefresh(ctx context.Context, client *redis.Client) (int64, error) {
	s, err := GetString(ctx, client, "esi:market_prices:next_refresh")
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// GetMarketPrice retrieves one market price JSON from a namespaced key.
func GetMarketPrice(ctx context.Context, client *redis.Client, typeID int32, target any) error {
	key := "esi:market_prices:" + url.PathEscape(strconv.FormatInt(int64(typeID), 10))
	return GetJSON(ctx, client, key, target)
}

// AcquireRefreshLock attempts to acquire a distributed lock for refresh operations.
// Returns true if the lock was acquired, a cleanup function to release the lock, and any error.
// The lock has a 300-second TTL to prevent deadlocks if a worker crashes.
// If lock is not acquired, cleanup will be nil.
func AcquireRefreshLock(ctx context.Context, client *redis.Client, lockKey string) (bool, func(), error) {
	lockAcquired, err := client.SetNX(ctx, lockKey, time.Now().UnixMilli(), 300*time.Second).Result()
	if err != nil {
		return false, nil, err
	}
	if !lockAcquired {
		return false, nil, nil
	}

	cleanup := func() {
		_ = client.Del(ctx, lockKey).Err()
	}
	return true, cleanup, nil
}
