package main

import (
	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func StartWSServer(redisClient *redis.Client, mongoClient *mongo.Client, natsConn *nats.Conn) error {
	cfg := config.LoadConfig()

	mux := http.NewServeMux()

	// health for quick checks
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logs.Error("failed to upgrade to websocket", "err", err)
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				logs.Error("failed to read message", "err", err)
				return
			}
			logs.Info("received message", "msg", string(msg))
			err = conn.WriteMessage(websocket.TextMessage, []byte("Hello, World!"))
			if err != nil {
				logs.Error("failed to write message", "err", err)
				return
			}
		}
	})

	logs.Info("ws server starting", "addr", ":"+cfg.WS_PORT)
	if err := http.ListenAndServe(":"+cfg.WS_PORT, mux); err != nil {
		logs.Error("failed to start websocket server", "err", err)
		return err
	}
	logs.Info("ws server started", "addr", ":"+cfg.WS_PORT)
	return nil
}
