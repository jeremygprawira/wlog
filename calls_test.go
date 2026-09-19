// This file tests the call records of one unit of work: the cap of 50, the totals in
// call_stats, a nested call of the same kind, a late end, a double end, and the rule that
// the text of an error never reaches a record.
package wlog_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// intOf reads a stored counter, treating a missing value as 0.
func intOf(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	default:
		return 0
	}
}

// callsOf returns the calls array of an event.
func callsOf(t *testing.T, event map[string]any) []any {
	t.Helper()
	calls, _ := event["calls"].([]any)
	return calls
}

// kindStatsOf returns the call_stats entry of one kind.
func kindStatsOf(t *testing.T, event map[string]any, kind string) map[string]any {
	t.Helper()
	stats, _ := event["call_stats"].(map[string]any)
	entry, _ := stats[kind].(map[string]any)
	if entry == nil {
		t.Fatalf("call_stats.%s is missing: %v", kind, event["call_stats"])
	}
	return entry
}

// recordOfKind returns the one call record of a kind.
func recordOfKind(t *testing.T, calls []any, kind string) map[string]any {
	t.Helper()
	for _, item := range calls {
		record, _ := item.(map[string]any)
		if record["kind"] == kind {
			return record
		}
	}
	t.Fatalf("no call record of kind %q in %v", kind, calls)
	return nil
}

// startUnit opens one event and returns its context plus the end func.
func startUnit(t *testing.T, opts ...wlog.Option) (context.Context, func(), *wlogtest.Recorder) {
	t.Helper()
	log, rec := wlogtest.New(t, opts...)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "unit")
	return ctx, end, rec
}

// TestCalls_DurationFromTheDriver proves that a call reports the duration its driver
// measured, and that a call without one keeps the measured duration.
func TestCalls_DurationFromTheDriver(t *testing.T) {
	ctx, end, rec := startUnit(t)

	_, callEnd := wlog.StartCall(ctx, wlog.Call{Kind: "db", Operation: "insert"})
	callEnd(wlog.CallResult{Status: "ok", Duration: 25 * time.Millisecond})
	_, measuredEnd := wlog.StartCall(ctx, wlog.Call{Kind: "cache", Operation: "get"})
	measuredEnd(wlog.CallResult{Status: "ok"})
	end()

	event := rec.Last()
	record := recordOfKind(t, callsOf(t, event), "db")
	if record["duration_ms"] != float64(25) {
		t.Errorf("calls[db].duration_ms = %v, want 25", record["duration_ms"])
	}
	stats := kindStatsOf(t, event, "db")
	if stats["duration_ms"] != float64(25) {
		t.Errorf("call_stats.db.duration_ms = %v, want 25", stats["duration_ms"])
	}
	cacheRecord := recordOfKind(t, callsOf(t, event), "cache")
	measured, ok := cacheRecord["duration_ms"].(float64)
	if !ok || measured < 0 {
		t.Errorf("calls[cache].duration_ms = %v, want a measured duration", cacheRecord["duration_ms"])
	}
}

// TestCalls_ErrTextNeverCopied proves that a record holds the error code and only the
// message an adapter marked safe. The text of the error never lands in the record.
func TestCalls_ErrTextNeverCopied(t *testing.T) {
	ctx, end, rec := startUnit(t, wlog.WithErrorExtractor(customExtractor{}))

	_, callEnd := wlog.StartCall(ctx, wlog.Call{Kind: "db", Operation: "SELECT"})
	callEnd(wlog.CallResult{ErrCode: "42P01", ErrMessage: "undefined table", Err: errors.New("secret detail")})

	// A call with no code of its own takes the code the extractor finds.
	_, secondEnd := wlog.StartCall(ctx, wlog.Call{Kind: "http", Operation: "GET"})
	secondEnd(wlog.CallResult{Err: errAny{}})
	end()

	calls := callsOf(t, rec.Last())
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	first, _ := calls[0].(map[string]any)
	info, _ := first["error"].(map[string]any)
	if info["code"] != "42P01" || info["message"] != "undefined table" {
		t.Errorf("error = %v, want the code and the safe message", info)
	}
	if text := fmt.Sprint(first); strings.Contains(text, "secret detail") {
		t.Errorf("the record copied the error text: %s", text)
	}

	second, _ := calls[1].(map[string]any)
	secondInfo, _ := second["error"].(map[string]any)
	if secondInfo["code"] != "ORDER_NOT_FOUND" {
		t.Errorf("error.code = %v, want the code from the extractor", secondInfo["code"])
	}
	if secondInfo["message"] != nil {
		t.Errorf("error.message = %v, want it absent when the adapter named no safe message", secondInfo["message"])
	}
}

// TestCalls_Cap50Stats60 proves the calls array stops at 50 while call_stats counts every
// call, and that the ten records past the cap are counted in wlog.dropped_calls.
func TestCalls_Cap50Stats60(t *testing.T) {
	ctx, end, rec := startUnit(t)

	for i := 0; i < 60; i++ {
		_, callEnd := wlog.StartCall(ctx, wlog.Call{Kind: "db", System: "postgresql", Operation: "SELECT", Target: "orders"})
		callEnd(wlog.CallResult{Status: "OK", Rows: 1})
	}
	end()

	got := rec.Last()
	if calls := callsOf(t, got); len(calls) != 50 {
		t.Errorf("calls holds %d records, want the cap of 50", len(calls))
	}
	stats := kindStatsOf(t, got, "db")
	if intOf(stats["count"]) != 60 {
		t.Errorf("call_stats.db.count = %v, want every call", stats["count"])
	}
	if intOf(stats["errors"]) != 0 {
		t.Errorf("call_stats.db.errors = %v, want 0", stats["errors"])
	}
	if intOf(counters(got)["dropped_calls"]) != 10 {
		t.Errorf("wlog.dropped_calls = %v, want 10", counters(got)["dropped_calls"])
	}
}

// TestCalls_NestedSameKindSkips proves an inner call of the same kind records nothing, so
// an adapter over a wrapped driver counts one call. A nested call of another kind still
// records, and the inner call keeps the span id of the call it sits in.
func TestCalls_NestedSameKindSkips(t *testing.T) {
	ctx, end, rec := startUnit(t)

	outerCtx, outerEnd := wlog.StartCall(ctx, wlog.Call{Kind: "db", Operation: "SELECT"})
	outerSpan, ok := wlog.CallSpanID(outerCtx)
	if !ok || outerSpan == "" {
		t.Fatal("CallSpanID returned no span id for an open call")
	}
	if call, ok := wlog.CallFromContext(outerCtx); !ok || call.Kind != "db" {
		t.Errorf("CallFromContext = %v, %v, want the open db call", call, ok)
	}

	innerCtx, innerEnd := wlog.StartCall(outerCtx, wlog.Call{Kind: "db", Operation: "SELECT"})
	if innerSpan, _ := wlog.CallSpanID(innerCtx); innerSpan != outerSpan {
		t.Errorf("the inner call took span %q, want the outer span %q", innerSpan, outerSpan)
	}
	innerEnd(wlog.CallResult{Status: "OK"})

	otherCtx, otherEnd := wlog.StartCall(outerCtx, wlog.Call{Kind: "http", Operation: "GET"})
	otherEnd(wlog.CallResult{Status: "200"})
	_ = otherCtx
	outerEnd(wlog.CallResult{Status: "OK", Rows: 3})
	end()

	calls := callsOf(t, rec.Last())
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want the outer db call and the http call", len(calls))
	}
	// A record is appended when its call ends, so the test looks the record up by kind.
	db := recordOfKind(t, calls, "db")
	if intOf(db["rows"]) != 3 {
		t.Errorf("the db record = %v, want the outer call's result", db)
	}
	if db["span_id"] != outerSpan {
		t.Errorf("record span_id = %v, want the open call's span", db["span_id"])
	}
	if recordOfKind(t, calls, "http")["status"] != "200" {
		t.Errorf("the http record = %v, want the nested call of another kind", recordOfKind(t, calls, "http"))
	}

	// Outside any event, StartCall is a no-op that records nothing.
	_, noop := wlog.StartCall(context.Background(), wlog.Call{Kind: "db"})
	noop(wlog.CallResult{Status: "OK"})
	if len(callsOf(t, rec.Last())) != 2 {
		t.Error("a call outside an event changed the recorded calls")
	}
}

// TestCalls_LateEndRecordsNothing proves an end func that runs after the event emitted
// records nothing, and that debug mode names the late write.
func TestCalls_LateEndRecordsNothing(t *testing.T) {
	problems := make(chan wlog.Problem, 4)
	ctx, end, rec := startUnit(t,
		wlog.WithDebug(true),
		wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
	)

	_, callEnd := wlog.StartCall(ctx, wlog.Call{Kind: "db", Operation: "SELECT"})
	end()
	callEnd(wlog.CallResult{Status: "OK"})

	got := rec.Last()
	if _, ok := got["calls"]; ok {
		t.Errorf("a late end recorded a call: %v", got["calls"])
	}
	close(problems)
	found := false
	for p := range problems {
		if p.Code == "WLOG_LATE_WRITE" {
			found = true
		}
	}
	if !found {
		t.Error("a late end reported no WLOG_LATE_WRITE in debug mode")
	}
}

// TestCalls_EndTwiceOnce proves calling the end func twice records one call.
func TestCalls_EndTwiceOnce(t *testing.T) {
	ctx, end, rec := startUnit(t)

	_, callEnd := wlog.StartCall(ctx, wlog.Call{Kind: "db", Operation: "SELECT"})
	callEnd(wlog.CallResult{Status: "OK", Rows: 1})
	callEnd(wlog.CallResult{Status: "ERROR"})
	end()

	calls := callsOf(t, rec.Last())
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	if record["status"] != "OK" {
		t.Errorf("status = %v, want the first result", record["status"])
	}
	if stats := kindStatsOf(t, rec.Last(), "db"); intOf(stats["count"]) != 1 {
		t.Errorf("call_stats.db.count = %v, want 1", stats["count"])
	}
}

// TestCalls_AttrsRedacted proves the attributes of a call pass through the redactor, so a
// call record never leaks a value the event hides.
func TestCalls_AttrsRedacted(t *testing.T) {
	ctx, end, rec := startUnit(t)

	_, callEnd := wlog.StartCall(ctx, wlog.Call{Kind: "db", Operation: "SELECT"})
	callEnd(wlog.CallResult{Status: "OK", Attrs: map[string]any{"token": "hunter2", "table": "orders"}})
	end()

	calls := callsOf(t, rec.Last())
	record, _ := calls[0].(map[string]any)
	attrs, _ := record["attrs"].(map[string]any)
	if attrs["token"] != "[REDACTED]" {
		t.Errorf("attrs.token = %v, want the redaction marker", attrs["token"])
	}
	if attrs["table"] != "orders" {
		t.Errorf("attrs.table = %v, want the plain value", attrs["table"])
	}
	if text := fmt.Sprint(rec.Last()); strings.Contains(text, "hunter2") {
		t.Errorf("the event leaked the secret: %s", text)
	}
}
