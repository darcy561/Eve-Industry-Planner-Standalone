package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"eveindustryplanner.com/esiworkers/redisDB"
	"github.com/redis/go-redis/v9"
)

func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		ctx := context.Background()

		key := fmt.Sprintf("ratelimit:%s", ip)

		current, err := redisDB.GetKeyAsInt(ctx, key)

		if err != nil && err != redis.Nil {
			log.Printf("Error checking rate limit: %v", ip)
			http.Error(w, "Error checking rate limit", http.StatusInternalServerError)
			return
		}

		if current >= 5 {
			log.Printf("Too many requests: %v", ip)
			http.Error(w, "Too many requests", http.StatusInternalServerError)
			return
		}

		redisDB.IncrementRedisValue(ctx, key)
		redisDB.SetKeyExpirey(ctx, key, time.Second*1)

		next.ServeHTTP(w, r)
	})
}
