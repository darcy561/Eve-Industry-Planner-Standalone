package main

import (
	"time"

	"eve-industry-planner/internal/tasks/esi"

	"github.com/nats-io/nats.go/jetstream"
)

// jetstreamMessageWrapper wraps jetstream.Msg to implement MessageInterface for handlers
type jetstreamMessageWrapper struct {
	msg jetstream.Msg
}

func (w *jetstreamMessageWrapper) Ack() error {
	return w.msg.Ack()
}

func (w *jetstreamMessageWrapper) Nak() error {
	return w.msg.Nak()
}

func (w *jetstreamMessageWrapper) Term() error {
	return w.msg.Term()
}

func (w *jetstreamMessageWrapper) InProgress() error {
	return w.msg.InProgress()
}

func (w *jetstreamMessageWrapper) NakWithDelay(delay time.Duration) error {
	return w.msg.NakWithDelay(delay)
}

func (w *jetstreamMessageWrapper) NumDelivered() uint64 {
	md, err := w.msg.Metadata()
	if err != nil {
		return 1
	}
	return md.NumDelivered
}

// wrapJetStreamMsg wraps a jetstream.Msg to esi.MessageInterface
func wrapJetStreamMsg(msg jetstream.Msg) esi.MessageInterface {
	return &jetstreamMessageWrapper{msg: msg}
}
