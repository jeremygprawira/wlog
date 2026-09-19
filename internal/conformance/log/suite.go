// Package logconformance is the conformance suite for every logger bridge, in both
// directions. Input folds a record made inside a unit of work into that event's logs[].
// Output writes one finished event as one record of the wrapped logger.
//
// The suite names the message and the attrs, and the bridge maps them onto its own
// library. The suite imports no logging library, so a slog, logrus, zap, or zerolog
// bridge passes the same scenarios.
package logconformance

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
)

// Factory builds one logger bridge in both directions.
type Factory interface {
	// Input logs one record through the bridge, with the context of the unit of work.
	Input(ctx context.Context, sink *Collector, message string, attrs map[string]any) error
	// Output returns a Logger whose every event goes through the bridge into sink.
	Output(sink *Collector) *wlog.Logger
}

// Record is one record a bridge wrote: the level name, the message, and the attrs.
type Record struct {
	Level   string
	Message string
	Attrs   map[string]any
}

// Collector remembers every record a bridge wrote, so a scenario reads the report.
type Collector struct {
	mu      sync.Mutex
	records []Record
}

// Record adds one written record.
func (c *Collector) Record(level, message string, attrs map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, Record{Level: level, Message: message, Attrs: attrs})
}

// Last returns the most recent written record, and false when the bridge wrote none.
func (c *Collector) Last() (Record, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.records) == 0 {
		return Record{}, false
	}
	return c.records[len(c.records)-1], true
}

// Run runs every scenario against the bridge the factory builds.
func Run(t conformance.TB, factory Factory) {
	t.Helper()
	t.Run("InputFolds", func(t conformance.TB) { testInputFolds(t, factory) })
	t.Run("InputPassesThrough", func(t conformance.TB) { testInputPassesThrough(t, factory) })
	t.Run("InputCapAndDropped", func(t conformance.TB) { testInputCapAndDropped(t, factory) })
	t.Run("OutputRecord", func(t conformance.TB) { testOutputRecord(t, factory) })
	t.Run("OutputKeepsOwnKeys", func(t conformance.TB) { testOutputKeepsOwnKeys(t, factory) })
	t.Run("SecretNeverCaptured", func(t conformance.TB) { testSecretNeverCaptured(t, factory) })
}

// startEvent opens one event on a recorder and returns its context plus the end func.
func startEvent(rec *conformance.MemoryRecorder) (context.Context, func()) {
	return wlog.Start(rec.Logger().WithContext(context.Background()), "checkout")
}

// logs returns the logs array of the single event of one run.
func logs(t conformance.TB, rec *conformance.MemoryRecorder) []any {
	t.Helper()
	lines, _ := rec.Last()["logs"].([]any)
	return lines
}

// testInputFolds proves that a record made inside a unit of work lands in logs[] with its
// level, its message, and its attrs.
func testInputFolds(t conformance.TB, factory Factory) {
	const name = "InputFolds"
	rec := conformance.NewMemoryRecorder()
	ctx, end := startEvent(rec)

	if err := factory.Input(ctx, &Collector{}, "hello", map[string]any{"k": "v"}); err != nil {
		t.Errorf("%s: Input returned %v", name, err)
	}
	end()

	lines := logs(t, rec)
	if len(lines) != 1 {
		t.Errorf("%s: logs = %d, want 1", name, len(lines))
		return
	}
	line, _ := lines[0].(map[string]any)
	if line["level"] != "info" || line["msg"] != "hello" {
		t.Errorf("%s: level/msg = %v/%v, want info/hello", name, line["level"], line["msg"])
	}
	attrs, _ := line["attrs"].(map[string]any)
	if attrs == nil || attrs["k"] != "v" {
		t.Errorf("%s: attrs = %v, want k=v", name, line["attrs"])
	}
}

// testInputPassesThrough proves that a record with no event in the context reaches the
// wrapped logger unchanged, so wiring a bridge never swallows a line.
func testInputPassesThrough(t conformance.TB, factory Factory) {
	const name = "InputPassesThrough"
	collector := &Collector{}

	if err := factory.Input(context.Background(), collector, "plain line", nil); err != nil {
		t.Errorf("%s: Input returned %v", name, err)
	}
	record, ok := collector.Last()
	if !ok {
		t.Errorf("%s: the wrapped logger received no record", name)
		return
	}
	if record.Message != "plain line" {
		t.Errorf("%s: message = %q, want the plain line", name, record.Message)
	}
}

// testInputCapAndDropped proves that logs[] stops at 50 lines, and that the rest count in
// wlog.dropped_logs.
func testInputCapAndDropped(t conformance.TB, factory Factory) {
	const name = "InputCapAndDropped"
	rec := conformance.NewMemoryRecorder()
	ctx, end := startEvent(rec)
	for i := 0; i < 60; i++ {
		_ = factory.Input(ctx, &Collector{}, "line", nil)
	}
	end()

	lines := logs(t, rec)
	if len(lines) != 50 {
		t.Errorf("%s: logs = %d, want 50", name, len(lines))
	}
	got := rec.Last()
	if counters, _ := got["wlog"].(map[string]any); !conformance.Equal(counters["dropped_logs"], 10) {
		t.Errorf("%s: wlog.dropped_logs = %v, want 10", name, got["wlog"])
	}
}

// testOutputRecord proves that one finished event becomes one record, with the operation
// as the message, the level mapped, and a nested group kept.
func testOutputRecord(t conformance.TB, factory Factory) {
	const name = "OutputRecord"
	collector := &Collector{}
	log := factory.Output(collector)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "checkout")
	wlog.SetGroup(ctx, "user", "id", "u-1")
	end()
	_ = log.Flush(context.Background())

	record, ok := collector.Last()
	if !ok {
		t.Errorf("%s: no record was written", name)
		return
	}
	if !strings.EqualFold(record.Level, "info") || record.Message != "checkout" {
		t.Errorf("%s: level/message = %q/%q, want info/checkout", name, record.Level, record.Message)
	}
	user, _ := record.Attrs["user"].(map[string]any)
	if user == nil || user["id"] != "u-1" {
		t.Errorf("%s: attrs.user = %v, want a nested id of u-1", name, record.Attrs["user"])
	}
}

// testOutputKeepsOwnKeys proves that a user key named like a key of the bridge never
// overwrites the bridge's own message or level.
func testOutputKeepsOwnKeys(t conformance.TB, factory Factory) {
	const name = "OutputKeepsOwnKeys"
	collector := &Collector{}
	log := factory.Output(collector)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "checkout")
	wlog.Set(ctx, "msg", "user text")
	wlog.Set(ctx, "time", "user time")
	end()
	_ = log.Flush(context.Background())

	record, ok := collector.Last()
	if !ok {
		t.Errorf("%s: no record was written", name)
		return
	}
	if record.Message != "checkout" {
		t.Errorf("%s: message = %q, want the operation", name, record.Message)
	}
	if !strings.EqualFold(record.Level, "info") {
		t.Errorf("%s: level = %q, want info", name, record.Level)
	}
}

// testSecretNeverCaptured proves that a credential in an attribute never reaches the
// event or the written record.
func testSecretNeverCaptured(t conformance.TB, factory Factory) {
	const name = "SecretNeverCaptured"
	const secret = "hunter2"
	rec := conformance.NewMemoryRecorder()
	ctx, end := startEvent(rec)

	if err := factory.Input(ctx, &Collector{}, "hello", map[string]any{"password": secret}); err != nil {
		t.Errorf("%s: Input returned %v", name, err)
	}
	end()

	got := rec.Last()
	if got == nil {
		t.Errorf("%s: no event recorded", name)
		return
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Errorf("%s: marshal the event: %v", name, err)
		return
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("%s: the secret %q reached the event", name, secret)
	}
}
