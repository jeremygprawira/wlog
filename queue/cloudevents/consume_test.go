// This file runs the work conformance suite against the event path, and checks the receive side
// and the recover wrapper that only CloudEvents has.
package wlogcloudevents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestCloudEvents_C1_WorkConformance proves that the event path passes every scenario of the
// work suite.
func TestCloudEvents_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path of this adapter. The suite supplies the unit,
// because a CloudEvent carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestCloudEvents_C1_ReceiveFields proves that the observability service fills the messaging
// group and the cloudevents group from one received event.
func TestCloudEvents_C1_ReceiveFields(t *testing.T) {
	log, rec := wlogtest.New(t)
	event := newEvent()
	event.SetExtension("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	_, end := Observability(log).RecordCallingInvoker(context.Background(), &event)
	end(nil)

	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	if got["operation"] != "process orders.created" {
		t.Errorf("operation = %v, want process orders.created", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "cloudevents", "operation": "process", "destination": "orders.created",
	} {
		if messaging[key] != want {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	ce, _ := messaging["cloudevents"].(map[string]any)
	for key, want := range map[string]any{
		"event_id": "evt-1", "event_source": "orders", "event_type": "orders.created",
		"event_subject": "orders.created",
	} {
		if ce[key] != want {
			t.Errorf("messaging.cloudevents.%s = %v, want %v", key, ce[key], want)
		}
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the extension", trace["trace_id"])
	}
}

// TestCloudEvents_C1_RecoverPanic proves that the recover wrapper turns a panic into an error
// with a stack, so the event of a panicking function still ends.
func TestCloudEvents_C1_RecoverPanic(t *testing.T) {
	wrapped := Recover(func(context.Context, cloudevents.Event) error {
		panic("boom")
	})

	err := wrapped(context.Background(), newEvent())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Recover returned %v, want the panic as an error", err)
	}
	var stacked interface{ Stack() string }
	if !errors.As(err, &stacked) || stacked.Stack() == "" {
		t.Errorf("the error carries no stack: %v", err)
	}
}

// newEvent builds one CloudEvent with the fields of the tests.
func newEvent() cloudevents.Event {
	event := cloudevents.NewEvent()
	event.SetID("evt-1")
	event.SetSource("orders")
	event.SetType("orders.created")
	event.SetSubject("orders.created")
	event.SetTime(time.Now().Add(-2 * time.Second))
	return event
}
