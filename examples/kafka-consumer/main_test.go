// This file consumes one message through a fake reader and compares the event with the
// recipe's hand-written golden. The schema tool validates the golden against
// schema/event.v1.json.
package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogkafka "github.com/jeremygprawira/wlog/queue/kafkago"
)

// TestKafkaConsumer_GoldenEvent proves that one consumed message gives the event the recipe
// documents, and that Consume commits the message after the handler returns nil.
func TestKafkaConsumer_GoldenEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	reader := &fakeReader{message: message()}

	if err := wlogkafka.Consume(context.Background(), rec.Logger(), reader, handle); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !reader.committed {
		t.Error("the message was not committed after a successful handler")
	}
	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(golden(t)), got); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}
}

// TestKafkaConsumer_FailedHandlerDoesNotCommit proves that a failed handler leaves the message
// uncommitted, so the group delivers it again.
func TestKafkaConsumer_FailedHandlerDoesNotCommit(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	reader := &fakeReader{message: message()}

	err := wlogkafka.Consume(context.Background(), rec.Logger(), reader, func(context.Context, kafka.Message) error {
		return errString("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("Consume returned %v, want the handler error", err)
	}
	if reader.committed {
		t.Error("the message was committed after a failed handler")
	}
}

// message builds the one message of the test, with the time and the headers of a real
// delivery.
func message() kafka.Message {
	return kafka.Message{
		Topic: "orders", Partition: 2, Offset: 41, HighWaterMark: 43,
		Key:   []byte("ord-1"),
		Value: []byte("payload"),
		Time:  time.Now().Add(-2 * time.Second),
		Headers: []kafka.Header{
			{Key: "traceparent", Value: []byte("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")},
		},
	}
}

// fakeReader serves one message and records the commit, because no test can build a real
// kafka.Reader without a broker.
type fakeReader struct {
	message   kafka.Message
	committed bool
}

// FetchMessage returns the one message of the test.
func (r *fakeReader) FetchMessage(context.Context) (kafka.Message, error) { return r.message, nil }

// CommitMessages records the commit.
func (r *fakeReader) CommitMessages(context.Context, ...kafka.Message) error {
	r.committed = true
	return nil
}

// Config returns the reader configuration, which carries the consumer group.
func (r *fakeReader) Config() kafka.ReaderConfig { return kafka.ReaderConfig{GroupID: "orders"} }

// golden reads the recipe's hand-written event.
func golden(t *testing.T) map[string]any {
	t.Helper()
	body, err := os.ReadFile("testdata/event.json")
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	event := map[string]any{}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("parse the golden: %v", err)
	}
	return event
}

// errString is the plain error a test returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
