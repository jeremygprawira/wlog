// This file runs the work conformance suite against the consumer path, and checks the fields
// and the ack rule that only NATS has.
package wlognats

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestNats_C1_WorkConformance proves that the consumer event path passes every scenario of the
// work suite.
func TestNats_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path of the handlers. The suite supplies the unit,
// because a NATS message carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestNats_C1_CoreHandlerFields proves that a core message fills the messaging group from the
// subject and the subscription.
func TestNats_CoreHandlerFields(t *testing.T) {
	log, rec := wlogtest.New(t)
	msg := &nats.Msg{
		Subject: "orders.created",
		Header: nats.Header{
			"traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		},
		Sub: &nats.Subscription{Subject: "orders.*", Queue: "workers"},
	}

	Handler(log, func(context.Context, *nats.Msg) error { return nil })(msg)

	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	if got["operation"] != "process orders.created" {
		t.Errorf("operation = %v, want process orders.created", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "nats", "operation": "process", "destination": "orders.created",
		"consumer_group": "workers",
	} {
		if messaging[key] != want {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	natsFields, _ := messaging["nats"].(map[string]any)
	if natsFields["subscription"] != "orders.*" {
		t.Errorf("messaging.nats.subscription = %v, want orders.*", natsFields["subscription"])
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the header", trace["trace_id"])
	}
}

// TestNats_C1_JetStreamAckRule proves that the JetStream handler acks on success, naks on
// error, and terms an error that TermOn names.
func TestNats_JetStreamAckRule(t *testing.T) {
	refusal := errString("no retry can fix this")

	cases := []struct {
		name string
		fn   JetStreamHandlerFunc
		opts []Option
		want string
	}{
		{"success acks", func(context.Context, jetstream.Msg) error { return nil }, nil, "ack"},
		{"error naks", func(context.Context, jetstream.Msg) error { return errString("failed") }, nil, "nak"},
		{"term on error", func(context.Context, jetstream.Msg) error { return refusal }, []Option{TermOn(refusal)}, "term"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, _ := wlogtest.New(t)
			msg := &fakeJetStreamMsg{subject: "orders"}
			JetStreamHandler(log, tc.fn, tc.opts...)(msg)
			if msg.ack != tc.want {
				t.Errorf("ack = %q, want %q", msg.ack, tc.want)
			}
		})
	}
}

// TestNats_C1_JetStreamFields proves that the metadata fills the delivery count, the stream
// fields, and the lag.
func TestNats_JetStreamFields(t *testing.T) {
	log, rec := wlogtest.New(t)
	msg := &fakeJetStreamMsg{
		subject: "orders.created",
		header: nats.Header{
			"traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		},
		metadata: &jetstream.MsgMetadata{
			Sequence:     jetstream.SequencePair{Stream: 42, Consumer: 7},
			NumDelivered: 3,
			NumPending:   5,
			Timestamp:    time.Now().Add(-2 * time.Second),
			Stream:       "ORDERS",
			Consumer:     "workers",
		},
	}

	JetStreamHandler(log, func(context.Context, jetstream.Msg) error { return nil })(msg)

	got := rec.Last()
	messaging, _ := got["messaging"].(map[string]any)
	if !conformance.Equal(messaging["delivery_count"], 3) {
		t.Errorf("messaging.delivery_count = %v, want 3", messaging["delivery_count"])
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	natsFields, _ := messaging["nats"].(map[string]any)
	for key, want := range map[string]any{"stream": "ORDERS", "consumer": "workers", "sequence": 42} {
		if !conformance.Equal(natsFields[key], want) {
			t.Errorf("messaging.nats.%s = %v, want %v", key, natsFields[key], want)
		}
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

// process runs one unit of work through the event path with a recovered panic, so the
// conformance suite continues after the panic scenario. The real entries record a panic and
// raise it again, which is the rule of the track spec.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}
