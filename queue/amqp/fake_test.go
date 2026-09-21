// This file holds the fakes of the tests: an acknowledger that records the ack of one delivery,
// and a publisher that records the messages. No test can reach a broker.
package wlogamqp

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// fakeAcknowledger records the acknowledgement of one delivery.
type fakeAcknowledger struct {
	acks    int
	nacks   int
	requeue bool
}

// Ack records one positive acknowledgement.
func (a *fakeAcknowledger) Ack(uint64, bool) error {
	a.acks++
	return nil
}

// Nack records one negative acknowledgement and its requeue flag.
func (a *fakeAcknowledger) Nack(_ uint64, _, requeue bool) error {
	a.nacks++
	a.requeue = requeue
	return nil
}

// Reject records nothing, because the tests do not use it.
func (*fakeAcknowledger) Reject(uint64, bool) error { return nil }

// fakePublisher records every published message, and reports err when the test set one.
type fakePublisher struct {
	mu       sync.Mutex
	messages []amqp.Publishing
	keys     []string
	err      error
}

// PublishWithContext records one message and its routing key.
func (p *fakePublisher) PublishWithContext(_ context.Context, _, key string, _, _ bool, msg amqp.Publishing) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, msg)
	p.keys = append(p.keys, key)
	return nil
}

// last returns the last published message, and nil when the publisher sent none.
func (p *fakePublisher) last() *amqp.Publishing {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.messages) == 0 {
		return nil
	}
	return &p.messages[len(p.messages)-1]
}
