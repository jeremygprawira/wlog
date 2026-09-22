//go:build cgo

// This file holds the fakes of the tests: a consumer that serves messages from a slice and
// records the commits, and a sender that reports the delivery of every message at once.
package wlogconfluent

import (
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// message builds one message of one topic, partition, and offset.
func message(topic string, partition int32, offset int64) *kafka.Message {
	return &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: partition, Offset: kafka.Offset(offset)},
		Timestamp:      time.Now(),
	}
}

// fakeConsumer serves messages from a slice, and records the commits. It satisfies Consumer,
// because no test can build a real consumer without a broker.
type fakeConsumer struct {
	mu        sync.Mutex
	messages  []*kafka.Message
	readErr   error
	commitErr error
	commits   []*kafka.Message
	timeouts  int
}

// ReadMessage returns the next message, and readErr when the slice ends. It reports the
// empty poll of a timeout for the first timeouts calls.
func (c *fakeConsumer) ReadMessage(time.Duration) (*kafka.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timeouts > 0 {
		c.timeouts--
		return nil, kafka.NewError(kafka.ErrTimedOut, "timeout", false)
	}
	if len(c.messages) == 0 {
		return nil, c.readErr
	}
	msg := c.messages[0]
	c.messages = c.messages[1:]
	return msg, nil
}

// CommitMessage records one committed message.
func (c *fakeConsumer) CommitMessage(msg *kafka.Message) ([]kafka.TopicPartition, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.commitErr != nil {
		return nil, c.commitErr
	}
	c.commits = append(c.commits, msg)
	return []kafka.TopicPartition{msg.TopicPartition}, nil
}

// committed returns the committed messages.
func (c *fakeConsumer) committed() []*kafka.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*kafka.Message(nil), c.commits...)
}

// fakeSender records the messages of a produce and reports their delivery at once. It
// satisfies Sender, because no test can build a real producer without a broker.
type fakeSender struct {
	mu          sync.Mutex
	sent        []*kafka.Message
	sendErr     error
	reportErr   error
	eventErr    kafka.Event
	closeReport bool
}

// Produce records one message and queues one delivery report on the channel.
func (s *fakeSender) Produce(msg *kafka.Message, delivery chan kafka.Event) error {
	s.mu.Lock()
	s.sent = append(s.sent, msg)
	sendErr, reportErr, eventErr := s.sendErr, s.reportErr, s.eventErr
	s.mu.Unlock()
	if sendErr != nil {
		return sendErr
	}
	if s.closeReport {
		close(delivery)
		return nil
	}
	if eventErr != nil {
		delivery <- eventErr
		return nil
	}
	report := *msg
	report.TopicPartition.Error = reportErr
	delivery <- &report
	return nil
}

// last returns the message of the last produce, and nil when the sender sent none.
func (s *fakeSender) last() *kafka.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) == 0 {
		return nil
	}
	return s.sent[len(s.sent)-1]
}
