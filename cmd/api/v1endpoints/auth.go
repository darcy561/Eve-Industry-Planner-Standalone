package v1endpoints

import (
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

func AuthHandler(w http.ResponseWriter, r *http.Request, redisClient *redis.Client, mongoClient *mongo.Client, natsConn *nats.Conn) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Hello, Auth!"))
}
