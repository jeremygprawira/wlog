// This file holds the fakes of the tests: a subscriber that hands its messages to the callback,
// and a publisher that reports the result of every publish. No test can reach the service.
package wlogpubsub

import (
	"context"
	"sync"

	"cloud.google.com/go/pubsub/v2"
)

// fakeSubscriber hands its messages to the receive callback, and reports readErr after them.
type fakeSubscriber struct {
	mu       sync.Mutex
	id       string
	messages []*pubsub.Message
	readErr  error
}

// Receive calls the callback once per message, and returns readErr after them.
func (s *fakeSubscriber) Receive(ctx context.Context, f func(context.Context, *pubsub.Message)) error {
	s.mu.Lock()
	messages, readErr := s.messages, s.readErr
	s.messages = nil
	s.mu.Unlock()
	for _, msg := range messages {
		f(ctx, msg)
	}
	return readErr
}

// ID returns the subscription name.
func (s *fakeSubscriber) ID() string { return s.id }

// fakePublisher reports one result per publish.
type fakePublisher struct {
	mu       sync.Mutex
	id       string
	messages []*pubsub.Message
}

// Publish records one message and returns a result that the caller cancels. The tests cannot
// build a ready result, because the constructor of one is internal to the client library.
func (p *fakePublisher) Publish(_ context.Context, msg *pubsub.Message) *pubsub.PublishResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, msg)
	return &pubsub.PublishResult{}
}

// ID returns the topic name.
func (p *fakePublisher) ID() string { return p.id }

// last returns the last published message, and nil when the publisher sent none.
func (p *fakePublisher) last() *pubsub.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.messages) == 0 {
		return nil
	}
	return p.messages[len(p.messages)-1]
}
