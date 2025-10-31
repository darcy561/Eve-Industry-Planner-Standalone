package esi

import (
	"time"
)

// MessageInterface provides a common interface for JetStream messages.
// Handlers use this interface to work with jetstream.Msg messages.
type MessageInterface interface {
	Ack() error
	Nak() error
	Term() error
	InProgress() error
	NakWithDelay(delay time.Duration) error
	NumDelivered() uint64
}
