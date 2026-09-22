// This file runs the drain conformance suite against the Kafka drain, and checks what only
// this drain has: the event JSON on the wire of a batch, and the setup factory that reads
// the environment.
package wlogkafkago

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	drainconformance "github.com/jeremygprawira/wlog/internal/conformance/drain"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/setup"
)

// TestKafka_C1_DrainShipsEvents proves that the drain passes the core part of the drain
// suite: the event reaches the topic in the v2 shape, and a redacted value never does.
func TestKafka_C1_DrainShipsEvents(t *testing.T) {
	broker := &fakeBroker{}
	drainconformance.Run(conformance.Tester{T: t}, []drainconformance.Case{{
		Name: "kafka",
		Build: func(*httpfake.Server) (wlog.Drain, error) {
			return Drain(&kafka.Writer{
				Addr:         kafka.TCP("broker:9092"),
				Topic:        "events",
				Transport:    broker,
				BatchTimeout: time.Millisecond,
			}), nil
		},
		Local: func(*httpfake.Server) []byte { return broker.bodies() },
	}})
}

// TestKafka_C1_FactoryReadsEnv proves that the setup factory names the two variables of the
// Kafka drain, builds a drain when both are set, and reports an error when one is missing.
func TestKafka_FactoryReadsEnv(t *testing.T) {
	factory := Factory()
	if factory.Name != "kafka" {
		t.Errorf("factory name = %q, want kafka", factory.Name)
	}
	for _, name := range []string{"WLOG_KAFKA_BROKERS", "WLOG_KAFKA_TOPIC"} {
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
	drain, err := factory.New(setup.MapEnv{
		"WLOG_KAFKA_BROKERS": "broker:9092",
		"WLOG_KAFKA_TOPIC":   "events",
	})
	if err != nil || drain == nil {
		t.Fatalf("New = %v, %v, want a drain", drain, err)
	}
}

// TestKafka_C1_DrainBatch proves that a batch of three events becomes three Kafka records,
// each one the canonical event JSON of its own event.
func TestKafka_DrainBatch(t *testing.T) {
	broker := &fakeBroker{}
	w := &kafka.Writer{
		Addr:         kafka.TCP("broker:9092"),
		Topic:        "events",
		Transport:    broker,
		BatchTimeout: time.Millisecond,
	}
	log := wlog.New(wlog.WithSilent(), wlog.WithRedactFingerprint(false), wlog.WithDrains(Drain(w)))

	golden := map[string]map[string]any{}
	for _, name := range []string{"first", "second", "third"} {
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.Set(ctx, "name", name)
		end()
		// The hand-written canonical event of one plain unit of work, with the values
		// that change between runs left out.
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

	msgs := broker.messages()
	if len(msgs) != 3 {
		t.Fatalf("records = %d, want 3", len(msgs))
	}
	for _, msg := range msgs {
		var event map[string]any
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			t.Fatalf("a record value is not JSON: %v", err)
		}
		name, _ := event["name"].(string)
		want, ok := golden[name]
		if !ok {
			t.Fatalf("unexpected event name %q", name)
		}
		if diff := conformance.Diff(want, conformance.Normalize(event)); diff != "" {
			t.Errorf("the record for %s differs from the golden:\n%s", name, diff)
		}
	}
}
