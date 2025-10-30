package messagebroker

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"eveindustryplanner.com/esiworkers/logger"
	"github.com/nats-io/nats.go"
)

type natsClient struct {
	conn *nats.Conn
	log  *slog.Logger
}

type MessageSubject string

const (
	AdjustedPrices MessageSubject = "refreshAdjustedPrices"
	MarketHistory  MessageSubject = "refreshMarketHistory"
	MarketPrices   MessageSubject = "refreshMarketPrices"
	SystemIndexes  MessageSubject = "refreshSystemIndexes"
)

var (
	instance *natsClient
	once     sync.Once
)

const (
	maxRetries = 5
	retryDelay = 2 * time.Second
)

// Sets up a connection to the NATS server
func Connect() {
	once.Do(func() {

		log := logger.GetLogger(context.Background(), logger.MessageBroker)

		natsURL := os.Getenv("NATS_URL")
		if natsURL == "" {
			natsURL = "nats:4222"
		}

		var nc *nats.Conn
		var err error

		for attempt := range maxRetries {
			nc, err = nats.Connect(natsURL)
			if err == nil {
				instance = &natsClient{conn: nc, log: log}
				log.Info("Connected to NATS service")
				return
			}

			attempt++
			message := fmt.Sprintf("Failed to connect to NATS, attempt %d of %d: %v", attempt, maxRetries, err.Error())
			log.Warn(message)
			time.Sleep(retryDelay)
		}

		message := fmt.Sprintf("Failed to connect to NATS after %d attempts: %v", maxRetries, err.Error())
		log.Error(message)
	})
}

// Closes the connection to the NATS server
func Disconnect() {
	if instance.conn != nil {
		instance.conn.Close()
		instance = nil
		instance.log.Info("Disconnected from NATS service")
	}
}

// Returns the instance of the NATS server connection
func GetInstance() *natsClient {
	if instance == nil {
		instance.log.Error("NATS service is not initialised")
	}
	return instance
}

func SubscribeToMessages(subject MessageSubject, handler nats.MsgHandler) {

	_, err := instance.conn.Subscribe(string(subject), handler)

	if err != nil {
		message := fmt.Sprintf("Failed to subscribe to subject %s: %v", subject, err.Error())
		instance.log.Error(message)
		return
	}

	log.Printf("Subscribed to subject %s", subject)
}

func PublishMessage(subject MessageSubject, message []byte) error {

	err := instance.conn.Publish(string(subject), message)

	if err != nil {
		message := fmt.Sprintf("Error publishing message: %v", err)
		instance.log.Error(message)
		return err
	}
	return nil
}

func Flush() error {
	err := instance.conn.Flush()
	if err != nil {
		return err
	}
	return nil
}
