package nats

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"

	natslib "github.com/nats-io/nats.go"
)

// ConnectNATS establishes a connection and returns it.
func Connect() (*natslib.Conn, error) {
	cfg := config.LoadConfig()

	retryCount, err := strconv.Atoi(cfg.NATSRetryCount)
	if err != nil {
		retryCount = 5
	}

	retryDelay, err := strconv.Atoi(cfg.NATSRetryDelay)
	if err != nil {
		retryDelay = 5
	}

	for i := 0; i < retryCount; i++ {
		conn, err := natslib.Connect(cfg.NATSURL, natslib.Timeout(5*time.Second))
		if err == nil {
			return conn, nil
		}
		i++
		message := fmt.Sprintf("Failed to connect to NATS. Attempt %d/%d. Error: %v", i, retryCount, err.Error())
		logs.Error(message)
		time.Sleep(time.Duration(retryDelay) * time.Second)
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
