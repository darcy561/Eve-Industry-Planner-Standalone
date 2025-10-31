package nats

import (
	"errors"
	"fmt"
	"time"

	"eve-industry-planner/internal/core/config"
	"eve-industry-planner/internal/shared/logs"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Connect establishes a connection and returns it.
func Connect() (*natslib.Conn, error) {
	cfg := config.LoadConfig()

	retryCount := 5
	retryDelay := 5 * time.Second

	for i := 0; i < retryCount; i++ {
		conn, err := natslib.Connect(cfg.NATS_URL, natslib.Timeout(retryDelay))
		if err == nil {
			i++
			message := fmt.Sprintf("Connected to NATS on attempt %d/%d", i, retryCount)
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

// ConnectJetStream establishes a connection and returns both the connection and JetStream context.
// This is a convenience function for services that need both.
func ConnectJetStream() (*natslib.Conn, jetstream.JetStream, error) {
	conn, err := Connect()
	if err != nil {
		return nil, nil, err
	}

	js, err := GetJetStream(conn)

	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	return conn, js, err
}

// GetJetStream returns a JetStream context from the connection using the new API.
// Use this when you already have a connection.
func GetJetStream(conn *natslib.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}
	return js, nil
}

// Cleanup drains and closes the provided NATS connection.
func Cleanup(conn *natslib.Conn) {
	if conn == nil {
		return
	}
	_ = conn.Drain()
	conn.Close()
}

// MessageAcker interface for messages that can be acknowledged.
// Used by NackWithBackoff to work with JetStream messages.
type MessageAcker interface {
	Ack() error
	Nak() error
	Term() error
	InProgress() error
	NakWithDelay(delay time.Duration) error
	NumDelivered() uint64
}

// NackWithBackoff performs a NAK with exponential backoff based on delivery count,
// and terminates the message after a maximum number of deliveries.
// Backoff schedule: 1s, 2s, 4s, 8s, ... capped at 60s.
func NackWithBackoff(msg MessageAcker) {
	const maxDeliveries = 5
	deliveries := msg.NumDelivered()
	if deliveries >= maxDeliveries {
		logs.Warn("nats message terminated after max deliveries", "deliveries", deliveries, "reason", "max_retries_exceeded")
		_ = msg.Term()
		return
	}
	delaySecs := 1 << (deliveries - 1)
	if delaySecs > 60 {
		delaySecs = 60
	}
	logs.Warn("nats message nak with backoff", "deliveries", deliveries, "delay_secs", delaySecs, "reason", "retry_with_backoff")
	_ = msg.NakWithDelay(time.Duration(delaySecs) * time.Second)
}
