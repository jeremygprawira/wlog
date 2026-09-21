//go:build cgo

// This file runs the work conformance suite against the consumer path, and checks the commit
// rule and the fields that only confluent has.
package wlogconfluent

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestConfluent_C1_WorkConformance proves that the consumer event path passes every scenario
// of the work suite.
func TestConfluent_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path Consume uses. The suite supplies the unit,
// because a Kafka message carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestConfluent_C1_HandlerErrorStopsTheLoop proves that the loop commits a message after the
// handler returns nil, and stops on a handler error before the next message is committed.
func TestConfluent_C1_HandlerErrorStopsTheLoop(t *testing.T) {
	good := message("orders", 1, 7)
	bad := message("orders", 1, 8)
	c := &fakeConsumer{messages: []*kafka.Message{good, bad}, readErr: io.EOF}
	log, _ := wlogtest.New(t)

	err := Consume(context.Background(), log, c, func(_ context.Context, msg *kafka.Message) error {
		if msg == bad {
			return errString("handler failed")
		}
		return nil
	})
	if err == nil || err.Error() != "handler failed" {
		t.Fatalf("Consume returned %v, want the handler error", err)
	}
	committed := c.committed()
	if len(committed) != 1 || committed[0] != good {
		t.Errorf("committed = %v, want only the message of the successful handler", committed)
	}
}

// TestConfluent_C1_MessageFields proves that one message fills the messaging group and the
// trace carrier.
func TestConfluent_C1_MessageFields(t *testing.T) {
	msg := message("orders", 3, 41)
	msg.Timestamp = time.Now().Add(-2 * time.Second)
	msg.Headers = []kafka.Header{{
		Key:   "traceparent",
		Value: []byte("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
	}}
	c := &fakeConsumer{messages: []*kafka.Message{msg}, readErr: io.EOF}
	log, rec := wlogtest.New(t)

	_ = Consume(context.Background(), log, c, func(context.Context, *kafka.Message) error { return nil })

	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	if got["operation"] != "process orders" {
		t.Errorf("operation = %v, want process orders", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "kafka", "operation": "process", "destination": "orders",
		"partition": 3, "offset": 41,
	} {
		if !conformance.Equal(messaging[key], want) {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the header", trace["trace_id"])
	}
}

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }

// TestConfluent_C1_ConsumeReadEdges proves that the loop ignores a read timeout, ends on a
// commit error, and records no destination for a message with no topic.
func TestConfluent_C1_ConsumeReadEdges(t *testing.T) {
	succeed := func(context.Context, *kafka.Message) error { return nil }

	t.Run("timeout then message", func(t *testing.T) {
		c := &fakeConsumer{messages: []*kafka.Message{message("orders", 0, 1)}, readErr: io.EOF, timeouts: 1}
		log, _ := wlogtest.New(t)
		if err := Consume(context.Background(), log, c, succeed); !errors.Is(err, io.EOF) {
			t.Fatalf("Consume returned %v, want the read error", err)
		}
		if committed := c.committed(); len(committed) != 1 {
			t.Errorf("commits = %d, want 1 after one ignored timeout", len(committed))
		}
	})

	t.Run("commit error", func(t *testing.T) {
		c := &fakeConsumer{messages: []*kafka.Message{message("orders", 0, 1)}, commitErr: errString("commit failed")}
		log, _ := wlogtest.New(t)
		err := Consume(context.Background(), log, c, succeed)
		if err == nil || err.Error() != "commit failed" {
			t.Fatalf("Consume returned %v, want the commit error", err)
		}
	})

	t.Run("no topic", func(t *testing.T) {
		c := &fakeConsumer{messages: []*kafka.Message{{Timestamp: time.Now()}}, readErr: io.EOF}
		log, rec := wlogtest.New(t)
		_ = Consume(context.Background(), log, c, succeed)
		messaging, _ := rec.Last()["messaging"].(map[string]any)
		if _, present := messaging["destination"]; present {
			t.Errorf("messaging.destination = %v, want no destination for a message with no topic", messaging["destination"])
		}
	})
}

// process runs one unit of work through the event path with a recovered panic, so the
// conformance suite continues after the panic scenario. The real entries record a panic and
// raise it again, which is the rule of the track spec.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}
