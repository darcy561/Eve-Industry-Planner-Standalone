package config

import (
	"os"
)

type Config struct {
	MONGO_URL           string
	WS_URL              string
	NATS_URL            string
	REDIS_URL           string
	MINIO_URL           string
	API_PORT            string
	WS_PORT             string
	AuthSecret          string
	ExternalJWTSecret   string
	ExternalJWTIssuer   string
	ExternalJWTAudience string
}

func LoadConfig() Config {
	return Config{
		MONGO_URL:           getEnv("MONGO_URL", "mongodb://mongo:27017/eve_industry"),
		REDIS_URL:           getEnv("REDIS_URL", "redis:6379"),
		NATS_URL:            getEnv("NATS_URL", "nats://nats:4222"),
		MINIO_URL:           getEnv("MINIO_URL", "http://minio:9000"),
		API_PORT:            getEnv("API_PORT", "8080"),
		WS_PORT:             getEnv("WS_PORT", "8091"),
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
