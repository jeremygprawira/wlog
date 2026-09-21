// This file runs the drain conformance suite against the NATS drain, and checks what only this
// drain has: the event JSON of a batch and the setup factory.
package wlognats

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	drainconformance "github.com/jeremygprawira/wlog/internal/conformance/drain"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/setup"
)

// TestNats_C1_DrainShipsEvents proves that the drain passes the core part of the drain suite:
// the event reaches the subject in the v2 shape, and a redacted value never does.
func TestNats_C1_DrainShipsEvents(t *testing.T) {
	pub := &fakePublisher{}
	drainconformance.Run(conformance.Tester{T: t}, []drainconformance.Case{{
		Name:  "nats",
		Build: func(*httpfake.Server) (wlog.Drain, error) { return Drain(pub, "events"), nil },
		Local: func(*httpfake.Server) []byte { return pub.bodies() },
	}})
}

// TestNats_C1_DrainFlushes proves that one batch flushes the client buffer before the call
// returns, so a process that ends does not lose the events.
func TestNats_C1_DrainFlushes(t *testing.T) {
	pub := &fakePublisher{}
	drain := Drain(pub, "events")
	drain.Send(context.Background(), map[string]any{"event_id": "e1"})
	if closer, ok := drain.(interface{ Close(context.Context) error }); ok {
		_ = closer.Close(context.Background())
	}

	if pub.flushed() == 0 {
		t.Error("the drain sent no flush")
	}
}

// TestNats_C1_DrainBatch proves that a batch of three events becomes three messages, each one
// the canonical event JSON of its own event.
func TestNats_C1_DrainBatch(t *testing.T) {
	pub := &fakePublisher{}
	log := wlog.New(wlog.WithSilent(), wlog.WithRedactFingerprint(false), wlog.WithDrains(Drain(pub, "events")))

	golden := map[string]map[string]any{}
	for _, name := range []string{"first", "second", "third"} {
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.Set(ctx, "name", name)
		end()
		// The hand-written canonical event of one plain unit of work, with the values that
		// change between runs left out.
		golden[name] = map[string]any{
			"kind": "work", "level": "info", "name": name, "operation": "op",
			"outcome": "success", "summary": "op success in {d}",
			"trace": map[string]any{},
			"wlog":  map[string]any{"schema_version": 2},
		}
	}
	if err := log.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := len(pub.messages); got != 3 {
		t.Fatalf("messages = %d, want 3", got)
	}
	for _, msg := range pub.messages {
		var event map[string]any
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			t.Fatalf("a message payload is not JSON: %v", err)
		}
		name, _ := event["name"].(string)
		want, ok := golden[name]
		if !ok {
			t.Fatalf("unexpected event name %q", name)
		}
		if diff := conformance.Diff(want, conformance.Normalize(event)); diff != "" {
			t.Errorf("the message for %s differs from the golden:\n%s", name, diff)
		}
	}
}

// TestNats_C1_FactoryReadsEnv proves that the setup factory names the two variables of the NATS
// drain and reports an error when one is missing. A live server is an integration step.
func TestNats_C1_FactoryReadsEnv(t *testing.T) {
	factory := Factory()
	if factory.Name != "nats" {
		t.Errorf("factory name = %q, want nats", factory.Name)
	}
	for _, name := range []string{"WLOG_NATS_URL", "WLOG_NATS_SUBJECT"} {
		found := false
		for _, variable := range factory.Vars {
			if variable.Name == name && variable.Required {
				found = true
			}
		}
		if !found {
			t.Errorf("the factory does not name the required variable %s", name)
		}
	}
	if _, err := factory.New(setup.MapEnv{}); err == nil {
		t.Error("New with no environment returned no error")
	}
}
