package config

import (
	"os"
)

type Config struct {
	MongoURI            string
	MongoRetryCount     string
	MongoRetryDelay     string
	NATSURL             string
	NATSRetryCount      string
	NATSRetryDelay      string
	RedisURL            string
	RedisRetryCount     string
	RedisRetryDelay     string
	MinioEndpoint       string
	AuthSecret          string
	ExternalJWTSecret   string
	ExternalJWTIssuer   string
	ExternalJWTAudience string
}

func LoadConfig() Config {
	return Config{
		MongoURI:            getEnv("MONGO_URI", "mongodb://mongo:27017/eve_industry"),
		MongoRetryCount:     getEnv("MONGO_RETRY_COUNT", "5"),
		MongoRetryDelay:     getEnv("MONGO_RETRY_DELAY", "5"),
		NATSURL:             getEnv("NATS_URL", "nats://nats:4222"),
		NATSRetryCount:      getEnv("NATS_RETRY_COUNT", "5"),
		NATSRetryDelay:      getEnv("NATS_RETRY_DELAY", "5"),
		RedisURL:            getEnv("REDIS_URL", "redis://redis:6379"),
		RedisRetryCount:     getEnv("REDIS_RETRY_COUNT", "5"),
		RedisRetryDelay:     getEnv("REDIS_RETRY_DELAY", "5"),
		MinioEndpoint:       getEnv("MINIO_ENDPOINT", "http://minio:9000"),
		AuthSecret:          getEnv("AUTH_SECRET", "dev-secret-change"),
		ExternalJWTSecret:   getEnv("EXTERNAL_JWT_SECRET", "dev-external-secret"),
		ExternalJWTIssuer:   getEnv("EXTERNAL_JWT_ISSUER", ""),
		ExternalJWTAudience: getEnv("EXTERNAL_JWT_AUDIENCE", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
