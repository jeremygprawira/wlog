// Package callsconformance is the conformance suite for every outbound adapter: an HTTP
// client, a database driver, a cache, a queue producer, or an AI client. One operation
// gives one call record, so a reader sees where a unit of work spent its time.
//
// The suite describes one call, and the adapter makes it and reports the result. The
// adapter never copies a parameter, an argument, or a body into the record.
package callsconformance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
)

// Factory makes one outgoing call, and returns the error of the call. An adapter returns
// the error of the wrapped operation unchanged, and it reports a fault through log.
type Factory interface {
	Call(ctx context.Context, log *wlog.Logger, call wlog.Call, result wlog.CallResult) error
}

// Run runs every scenario against the adapter the factory builds.
func Run(t conformance.TB, factory Factory) {
	t.Helper()
	t.Run("Kinds", func(t conformance.TB) { testKinds(t, factory) })
	t.Run("FailedCall", func(t conformance.TB) { testFailedCall(t, factory) })
	t.Run("NoEvent", func(t conformance.TB) { testNoEvent(t, factory) })
	t.Run("CapAndStats", func(t conformance.TB) { testCapAndStats(t, factory) })
	t.Run("SecretNeverCaptured", func(t conformance.TB) { testSecretNeverCaptured(t, factory) })
}

// oneCall starts one event, makes one call inside it, and ends the event.
func oneCall(factory Factory, call wlog.Call, result wlog.CallResult) (*conformance.MemoryRecorder, error) {
	rec := conformance.NewMemoryRecorder()
	log := rec.Logger()
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	err := factory.Call(ctx, log, call, result)
	end()
	return rec, err
}

// testKinds proves that one call of each kind gives one record with the fields an adapter
// names, and that a call_stats entry counts it.
func testKinds(t conformance.TB, factory Factory) {
	cases := []struct {
		name string
		call wlog.Call
	}{
		{"http", wlog.Call{Kind: "http", System: "http", Operation: "GET", Target: "api.example.com"}},
		{"db", wlog.Call{Kind: "db", System: "postgresql", Operation: "SELECT", Target: "orders"}},
		{"cache", wlog.Call{Kind: "cache", System: "redis", Operation: "GET", Target: "session"}},
	}

	for _, tc := range cases {
		name := "Kinds/" + tc.name
		rec, err := oneCall(factory, tc.call, wlog.CallResult{Status: "OK"})
		if err != nil {
			t.Errorf("%s: Call returned %v", name, err)
		}
		calls, _ := rec.Last()["calls"].([]any)
		if len(calls) != 1 {
			t.Errorf("%s: calls = %d, want 1", name, len(calls))
			continue
		}
		record, _ := calls[0].(map[string]any)
		for key, want := range map[string]any{
			"kind": tc.call.Kind, "system": tc.call.System,
			"operation": tc.call.Operation, "target": tc.call.Target, "status": "OK",
		} {
			if record[key] != want {
				t.Errorf("%s: %s = %v, want %v", name, key, record[key], want)
			}
		}
		if _, present := record["duration_ms"]; !present {
			t.Errorf("%s: duration_ms is missing", name)
		}
	}
}

// testFailedCall proves that a failed call records a code, never the raw error text, and
// hands the app's own error value back unchanged.
func testFailedCall(t conformance.TB, factory Factory) {
	const name = "FailedCall"
	call := wlog.Call{Kind: "db", System: "postgresql", Operation: "INSERT", Target: "orders"}
	failure := errors.New("duplicate key value violates unique constraint")

	rec, err := oneCall(factory, call, wlog.CallResult{Err: failure, ErrCode: "23505"})
	if !errors.Is(err, failure) {
		t.Errorf("%s: Call returned %v, want the same error value", name, err)
	}
	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Errorf("%s: calls = %d, want 1", name, len(calls))
		return
	}
	record, _ := calls[0].(map[string]any)
	info, _ := record["error"].(map[string]any)
	if info == nil || !conformance.Equal(info["code"], "23505") {
		t.Errorf("%s: error.code = %v, want 23505", name, record["error"])
	}
	if _, present := info["message"]; present {
		t.Errorf("%s: error.message = %v, want no raw error text", name, info["message"])
	}
}

// testNoEvent proves that a call outside a unit of work records no event and reports
// nothing, because a call with no event is not a fault.
func testNoEvent(t conformance.TB, factory Factory) {
	const name = "NoEvent"
	rec := conformance.NewMemoryRecorder()

	if err := factory.Call(context.Background(), rec.Logger(), wlog.Call{Kind: "http"}, wlog.CallResult{}); err != nil {
		t.Errorf("%s: Call returned %v outside a unit of work", name, err)
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("%s: events = %d, want none", name, count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("%s: problems = %d, want none", name, count)
	}
}

// testCapAndStats proves that the calls array stops at 50 records, and that every call
// still counts in call_stats and in the dropped counter.
func testCapAndStats(t conformance.TB, factory Factory) {
	const name = "CapAndStats"
	rec := conformance.NewMemoryRecorder()
	log := rec.Logger()
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	call := wlog.Call{Kind: "db", System: "postgresql", Operation: "SELECT", Target: "orders"}
	for i := 0; i < 60; i++ {
		_ = factory.Call(ctx, log, call, wlog.CallResult{Status: "OK"})
	}
	end()

	got := rec.Last()
	if got == nil {
		t.Errorf("%s: no event recorded", name)
		return
	}
	calls, _ := got["calls"].([]any)
	if len(calls) != 50 {
		t.Errorf("%s: calls = %d, want 50", name, len(calls))
	}
	stats, _ := got["call_stats"].(map[string]any)
	kindStats, _ := stats["db"].(map[string]any)
	if kindStats == nil || !conformance.Equal(kindStats["count"], 60) {
		t.Errorf("%s: call_stats.db = %v, want a count of 60", name, stats["db"])
	}
	if counters, _ := got["wlog"].(map[string]any); !conformance.Equal(counters["dropped_calls"], 10) {
		t.Errorf("%s: wlog.dropped_calls = %v, want 10", name, got["wlog"])
	}
}

// testSecretNeverCaptured proves that a credential or a parameter in the attrs of a call
// never reaches the event.
func testSecretNeverCaptured(t conformance.TB, factory Factory) {
	const name = "SecretNeverCaptured"
	const secret = "hunter2"
	call := wlog.Call{Kind: "db", System: "postgresql", Operation: "SELECT", Target: "users"}
	result := wlog.CallResult{Attrs: map[string]any{"password": secret, "statement": "SELECT * FROM users"}}

	rec, _ := oneCall(factory, call, result)
	got := rec.Last()
	if got == nil {
		t.Errorf("%s: no event recorded", name)
		return
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s: marshal the event: %v", name, err)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("%s: the secret %q reached the event", name, secret)
	}
}
