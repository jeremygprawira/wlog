// This file holds the fakes of the tests: a consumer group session and claim that drive the
// claim loop, and a sync and an async producer that record what a send carried.
package wlogsarama

import (
	"context"
	"sync"

	"github.com/IBM/sarama"
)

// fakeSession is one consumer group session, which records the marked offsets.
type fakeSession struct {
	mu     sync.Mutex
	marked []*sarama.ConsumerMessage
}

// newFakeSession builds an empty session.
func newFakeSession() *fakeSession { return &fakeSession{} }

// Claims returns the claimed partitions, which the tests do not use.
func (*fakeSession) Claims() map[string][]int32 { return nil }

// MemberID returns the member id of the group.
func (*fakeSession) MemberID() string { return "member-1" }

// GenerationID returns the generation of the group.
func (*fakeSession) GenerationID() int32 { return 3 }

// MarkOffset records a marked offset, which the tests do not use.
func (*fakeSession) MarkOffset(string, int32, int64, string) {}

// Commit records a commit, which the tests do not use.
func (*fakeSession) Commit() {}

// ResetOffset records a rewind, which the tests do not use.
func (*fakeSession) ResetOffset(string, int32, int64, string) {}

// MarkMessage records one marked message.
func (s *fakeSession) MarkMessage(msg *sarama.ConsumerMessage, _ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked = append(s.marked, msg)
}

// Context returns the session context.
func (*fakeSession) Context() context.Context { return context.Background() }

// markedOffsets returns the offsets of the marked messages.
func (s *fakeSession) markedOffsets() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int64, 0, len(s.marked))
	for _, msg := range s.marked {
		out = append(out, msg.Offset)
	}
	return out
}

// fakeClaim is one claimed partition, which serves its messages from a slice.
type fakeClaim struct {
	topic         string
	partition     int32
	highWaterMark int64
	messages      []*sarama.ConsumerMessage
	channel       chan *sarama.ConsumerMessage
}

// Topic returns the topic of the claim.
func (c *fakeClaim) Topic() string { return c.topic }

// Partition returns the partition of the claim.
func (c *fakeClaim) Partition() int32 { return c.partition }

// InitialOffset returns the offset the claim started at.
func (*fakeClaim) InitialOffset() int64 { return 0 }

// HighWaterMarkOffset returns the offset of the next message of the claim.
func (c *fakeClaim) HighWaterMarkOffset() int64 { return c.highWaterMark }

// Messages returns the one channel of the claim, filled with its messages and closed. A
// claim hands out the same channel on every call, so the loop of the handler ends.
func (c *fakeClaim) Messages() <-chan *sarama.ConsumerMessage {
	if c.channel == nil {
		c.channel = make(chan *sarama.ConsumerMessage, len(c.messages))
		for _, msg := range c.messages {
			c.channel <- msg
		}
		close(c.channel)
	}
	return c.channel
}

// fakeSyncProducer records every message it sends.
type fakeSyncProducer struct {
	mu       sync.Mutex
	messages []*sarama.ProducerMessage
}

// SendMessage records one message.
func (p *fakeSyncProducer) SendMessage(msg *sarama.ProducerMessage) (int32, int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, msg)
	return 0, int64(len(p.messages)), nil
}

// SendMessages records a batch of messages.
func (p *fakeSyncProducer) SendMessages(msgs []*sarama.ProducerMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, msgs...)
	return nil
}

// Close closes nothing.
func (*fakeSyncProducer) Close() error { return nil }

// last returns the message of the last send, and nil when the producer sent none.
func (p *fakeSyncProducer) last() *sarama.ProducerMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.messages) == 0 {
		return nil
	}
	return p.messages[len(p.messages)-1]
}

// fakeAsyncProducer is an async producer whose result channels the test feeds.
type fakeAsyncProducer struct {
	input     chan *sarama.ProducerMessage
	successes chan *sarama.ProducerMessage
	failures  chan *sarama.ProducerError
}

// newFakeAsyncProducer builds a producer whose channels hold one result.
func newFakeAsyncProducer() *fakeAsyncProducer {
	return &fakeAsyncProducer{
		input:     make(chan *sarama.ProducerMessage, 1),
		successes: make(chan *sarama.ProducerMessage, 1),
		failures:  make(chan *sarama.ProducerError, 1),
	}
}

// AsyncClose closes the three channels, which ends the wrapper that reads them.
func (p *fakeAsyncProducer) AsyncClose() {
	close(p.input)
	close(p.successes)
	close(p.failures)
}

// Close closes the three channels.
func (p *fakeAsyncProducer) Close() error {
	p.AsyncClose()
	return nil
}

// Input returns the channel the wrapper writes to.
func (p *fakeAsyncProducer) Input() chan<- *sarama.ProducerMessage { return p.input }

// Successes returns the channel the test feeds a produced message to.
func (p *fakeAsyncProducer) Successes() <-chan *sarama.ProducerMessage { return p.successes }

// Errors returns the channel the test feeds a failure to.
func (p *fakeAsyncProducer) Errors() <-chan *sarama.ProducerError { return p.failures }
