package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"eve-industry-planner/cmd/api/middleware"
	"eve-industry-planner/cmd/api/v1endpoints"
	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/ulule/limiter/v3"
	lredis "github.com/ulule/limiter/v3/drivers/store/redis"
	"go.mongodb.org/mongo-driver/mongo"
)

type route struct {
	Path    string
	Handler http.HandlerFunc
}

func StartAPIServer(redisClient *redis.Client, mongoClient *mongo.Client, natsConn *nats.Conn) error {
	cfg := config.LoadConfig()

	if os.Getenv("FAIL_ON_STARTUP") == "true" {
		return fmt.Errorf("startup failure requested via FAIL_ON_STARTUP")
	}

	//creates rate limits for routes and setups up redis store to store them
	publicRateLimit, err := limiter.NewRateFromFormatted("5-S")
	if err != nil {
		logs.Error("failed to create public rate limiter", "err", err)
		return err
	}
	privateRateLimit, err := limiter.NewRateFromFormatted("100-M")
	if err != nil {
		logs.Error("failed to create private rate limiter", "err", err)
		return err
	}
	store, err := lredis.NewStoreWithOptions(redisClient, limiter.StoreOptions{
		Prefix:          "limiter",
		CleanUpInterval: 5 * time.Minute,
	})
	if err != nil {
		logs.Error("failed to create redis store", "err", err)
		return err
	}

	mux := http.NewServeMux()

	// Global middleware constructors, applied to all routes via groups
	globalConstructors := []middleware.MiddlewareConstructor{}

	// Public and private groups, with middleware constructors applied after global
	publicGroup := middleware.NewGroup(mux,
		append(globalConstructors,
			middleware.RateLimiterConstructor(store, publicRateLimit),
		)...,
	)
	privateGroup := middleware.NewGroup(mux,
		append(globalConstructors,
			middleware.RateLimiterConstructor(store, privateRateLimit),
		)...,
	)

	// Define public routes
	publicRoutes := []route{
		{
			Path: "/auth/v1",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				v1endpoints.AuthHandler(w, r, redisClient, mongoClient, natsConn)
			},
		},
		{Path: "/v1/systemindexes",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				v1endpoints.SystemIndexesHandler(w, r, redisClient)
			},
		},
	}
	// Register public routes
	for _, route := range publicRoutes {
		publicGroup.HandleFunc(route.Path, route.Handler)
	}

	// Define private routes
	privateRoutes := []route{
		{
			Path: "/private/v1",
			Handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Hello, Private!"))
			},
		},
	}
	// Register private routes
	for _, route := range privateRoutes {
		privateGroup.HandleFunc(route.Path, route.Handler)
	}

	logs.Info("api http server starting", "addr", ":"+cfg.API_PORT)
	if err := http.ListenAndServe(":"+cfg.API_PORT, mux); err != nil {
		logs.Error("api http server error", "err", err)
		return err
	}
	logs.Info("api service listening")
	return nil
}
