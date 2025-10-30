package nats

import (
	"errors"
	"fmt"
	"time"

	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"

	natslib "github.com/nats-io/nats.go"
)

// ConnectNATS establishes a connection and returns it.
func Connect() (*natslib.Conn, error) {
	cfg := config.LoadConfig()

	retryCount := 5
	retryDelay := 5 * time.Second

	for i := 0; i < retryCount; i++ {
		conn, err := natslib.Connect(cfg.NATS_URL, natslib.Timeout(retryDelay))
		if err == nil {
			i++
			message := fmt.Sprintf("Connected to Mongo on attempt %d/%d", i, retryCount)
			logs.Info(message)
			return conn, nil
		}
		i++
		message := fmt.Sprintf("Failed to connect to NATS. Attempt %d/%d. Error: %v", i, retryCount, err.Error())
		logs.Error(message)
		time.Sleep(retryDelay)
	}

	message := fmt.Sprintf("Failed to connect to NATS after %d attempts. Exiting...", retryCount)
	return nil, errors.New(message)
}

// Cleanup drains and closes the provided NATS connection.
func Cleanup(conn *natslib.Conn) {
	if conn == nil {
		return
	}
	_ = conn.Drain()
	conn.Close()
}
