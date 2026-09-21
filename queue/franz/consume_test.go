// This file runs the work conformance suite against the consumer path, and checks the
// fields of one polled record.
package wlogfranz

import (
	"context"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestFranz_C1_WorkConformance proves that the consumer event path passes every scenario of
// the work suite.
func TestFranz_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path Record uses. The suite supplies the unit,
// because a Kafka record carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestFranz_C1_RecordFields proves that one record fills the messaging group, the consumer
// group, and the trace carrier.
func TestFranz_C1_RecordFields(t *testing.T) {
	cl, err := kgo.NewClient(kgo.SeedBrokers("127.0.0.1:1"), kgo.ConsumerGroup("workers"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cl.Close()

	r := &kgo.Record{
		Topic: "orders", Partition: 3, Offset: 41, Timestamp: time.Now().Add(-2 * time.Second),
		Headers: []kgo.RecordHeader{{
			Key:   "traceparent",
			Value: []byte("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
		}},
	}
	log, rec := wlogtest.New(t)

	ctx, end := Record(log, cl, r)
	if ctx == nil {
		t.Fatal("Record returned no context")
	}
	end(nil)

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
		"partition": 3, "offset": 41, "consumer_group": "workers",
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

// process runs one unit of work through the event path with a recovered panic, so the
// conformance suite continues after the panic scenario. The real entries record a panic and
// raise it again, which is the rule of the track spec.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}
