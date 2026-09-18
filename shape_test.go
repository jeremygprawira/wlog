// This file tests the time semantics of an event and the fields of ErrorInfo, which
// phase 11 defines: the timestamp is the start of the work, the duration is a float
// with microsecond precision, and an error names its Go type, its causes, and the line
// that recorded it.
package wlog_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/redact"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestShape_CORE16_TimestampIsStart proves that the event carries the start of the
// work, not the moment it was written. A timestamp taken at emit time moves with the
// load on the writer and hides the real latency.
func TestShape_CORE16_TimestampIsStart(t *testing.T) {
	log, rec := wlogtest.New(t)
	start := time.Now()

	_, end := wlog.Start(log.WithContext(context.Background()), "checkout")
	time.Sleep(25 * time.Millisecond)
	end()

	raw, ok := rec.Last()["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp = %v, want a string", rec.Last()["timestamp"])
	}
	stamp, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("timestamp %q is not RFC 3339: %v", raw, err)
	}
	if stamp.After(start.Add(10 * time.Millisecond)) {
		t.Errorf("timestamp %s is %s after the start, so it names the emit time",
			stamp, stamp.Sub(start))
	}
}

// TestShape_CORE27_FloatDuration proves that duration_ms carries microseconds. An
// event that ends in under a millisecond used to report a plain zero, so a fast
// handler looked like work that never happened.
func TestShape_CORE27_FloatDuration(t *testing.T) {
	log, rec := wlogtest.New(t)

	_, end := wlog.Start(log.WithContext(context.Background()), "fast")
	end()

	got, ok := rec.Last()["duration_ms"].(float64)
	if !ok {
		t.Fatalf("duration_ms = %v, want a number", rec.Last()["duration_ms"])
	}
	if got <= 0 {
		t.Errorf("duration_ms = %v, want a value above zero for a finished event", got)
	}
	if got > 100 {
		t.Errorf("duration_ms = %v, want microseconds of real work", got)
	}
}

// TestShape_BET17_CallerTypeCauses proves that an error carries its Go type, the line
// that recorded it, and the messages of an errors.Join, and that WithCaller(false)
// leaves the line out.
func TestShape_BET17_CallerTypeCauses(t *testing.T) {
	t.Run("type and caller", func(t *testing.T) {
		log, rec := wlogtest.New(t)
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.Error(ctx, &catalogError{code: "E1"})
		end()

		info := errorObject(t, rec.Last())
		if got := info["type"]; got != "*wlog_test.catalogError" {
			t.Errorf("type = %v, want *wlog_test.catalogError", got)
		}
		caller, _ := info["caller"].(string)
		if !strings.HasPrefix(caller, "shape_test.go:") {
			t.Errorf("caller = %q, want the file and line of the wlog.Error call", caller)
		}
	})

	t.Run("causes", func(t *testing.T) {
		log, rec := wlogtest.New(t)
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.Error(ctx, errors.Join(errors.New("first cause"), errors.New("second cause")))
		end()

		info := errorObject(t, rec.Last())
		causes, _ := info["causes"].([]any)
		if len(causes) != 2 || causes[0] != "first cause" || causes[1] != "second cause" {
			t.Errorf("causes = %v, want the two joined messages", info["causes"])
		}
	})

	t.Run("caller off", func(t *testing.T) {
		log, rec := wlogtest.New(t, wlog.WithCaller(false))
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.Error(ctx, errors.New("plain"))
		end()

		if info := errorObject(t, rec.Last()); info["caller"] != nil {
			t.Errorf("caller = %v, want it absent with WithCaller(false)", info["caller"])
		}
	})
}

// TestShape_PAR7_PlainErrorHasType proves that an error built with errors.New still
// names its type, so a reader sees what kind of error reached the event.
func TestShape_PAR7_PlainErrorHasType(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Error(ctx, errors.New("plain failure"))
	end()

	info := errorObject(t, rec.Last())
	if got := info["type"]; got != "*errors.errorString" {
		t.Errorf("type = %v, want *errors.errorString", got)
	}
	if got := info["message"]; got != "plain failure" {
		t.Errorf("message = %v, want plain failure", got)
	}
}

// catalogError is a typed error, so the test can prove that the event names the Go
// type of the error rather than the type of a wrapper.
type catalogError struct{ code string }

func (e *catalogError) Error() string { return "catalog " + e.code }

// errorObject returns the error object of an event, and fails the test when the event
// carries none.
func errorObject(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	info, ok := event["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %v, want an object", event["error"])
	}
	return info
}

// TestShape_CORE34_WlogNested proves that the counters and the fingerprint live in one
// nested wlog object, and that no flat key pretends to be part of it.
func TestShape_CORE34_WlogNested(t *testing.T) {
	log, rec := wlogtest.New(t)
	_, end := wlog.Start(log.WithContext(context.Background()), "op")
	end()

	got := rec.Last()
	counters, ok := got["wlog"].(map[string]any)
	if !ok {
		t.Fatalf("wlog = %v, want the nested object", got["wlog"])
	}
	if got := fmt.Sprint(counters["schema_version"]); got != "2" {
		t.Errorf("wlog.schema_version = %v, want 2", counters["schema_version"])
	}
	if counters["redact_fingerprint"] == nil || counters["redact_fingerprint"] == "" {
		t.Errorf("wlog.redact_fingerprint = %v, want the fingerprint", counters["redact_fingerprint"])
	}
	if _, ok := counters["dropped_fields"]; ok {
		t.Errorf("wlog carries a zero counter: %v", counters)
	}
	for key := range got {
		if strings.HasPrefix(key, "wlog.") || key == "redact.fingerprint" {
			t.Errorf("flat key %q is still written", key)
		}
	}
}

// TestShape_EventIDIsUUIDv7 proves that each event carries a version 7 UUID, that two
// events differ, and that a detached child keeps the parent's trace id.
func TestShape_EventIDIsUUIDv7(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "parent")
	_, endChild := wlog.Detach(ctx, "child")
	endChild()
	end()

	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	seen := map[string]bool{}
	for _, event := range events {
		id, _ := event["event_id"].(string)
		if !pattern.MatchString(id) {
			t.Errorf("event_id = %q, want a version 7 UUID", id)
		}
		if seen[id] {
			t.Errorf("event_id %q appears twice", id)
		}
		seen[id] = true
	}
	parentTrace, _ := events[1]["trace"].(map[string]any)
	childTrace, _ := events[0]["trace"].(map[string]any)
	if parentTrace["trace_id"] == nil || parentTrace["trace_id"] != childTrace["trace_id"] {
		t.Errorf("child trace_id = %v, want the parent's %v", childTrace["trace_id"], parentTrace["trace_id"])
	}
	if childTrace["span_id"] == parentTrace["span_id"] {
		t.Error("the child shares the parent's span_id, want its own")
	}
}

// TestShape_EmptyValuesOmitted proves that an empty value costs no space, because the
// schema gives an absent value and an empty one the same meaning.
func TestShape_EmptyValuesOmitted(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON), wlog.WithDrains(memory.New(0)))
	out := captureStdout(t, func() {
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		wlog.Set(ctx, "empty", "")
		wlog.SetGroup(ctx, "group", map[string]any{})
		end()
	})

	for _, gone := range []string{`"empty"`, `"group"`, `"dropped_fields"`, "null"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s is in the line: %s", gone, out)
		}
	}
}

// TestShape_BET5_SummaryPanicFallsBack proves that a builder which panics costs the
// event nothing but the custom summary, and that the panic is reported.
func TestShape_BET5_SummaryPanicFallsBack(t *testing.T) {
	problems := make(chan wlog.Problem, 4)
	log, rec := wlogtest.New(t,
		wlog.WithSummary(func(wlog.Event) string { panic("summary boom") }),
		wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
	)

	_, end := wlog.Start(log.WithContext(context.Background()), "checkout")
	end()

	if got := rec.Last()["summary"]; got != "checkout success in 0.0ms" && !strings.HasPrefix(fmt.Sprint(got), "checkout success in ") {
		t.Errorf("summary = %v, want the default text", got)
	}
	select {
	case p := <-problems:
		if p.Code != "WLOG_HOOK_PANIC" || p.Source != "summary" {
			t.Errorf("report = %+v, want WLOG_HOOK_PANIC from the summary", p)
		}
	default:
		t.Error("a panicking summary builder reported nothing")
	}
}

// TestShape_BET5_MaskedValueStaysOut proves that a value the redactor hid cannot come
// back inside the summary of the event that hid it.
func TestShape_BET5_MaskedValueStaysOut(t *testing.T) {
	// order_id is an id key, so the summary would carry it, and this redactor masks
	// it, so the summary must leave it out instead of showing the mask.
	log := wlog.New(
		wlog.WithFormat(wlog.FormatJSON),
		wlog.WithDrains(memory.New(0)),
		wlog.WithRedactor(redact.MustNew(redact.AddKeys("order_id"))),
	)
	out := captureStdout(t, func() {
		ctx, end := wlog.Start(log.WithContext(context.Background()), "checkout")
		wlog.Set(ctx, "order_id", "hunter2-secret")
		end()
	})

	summary := ""
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, `"summary"`) {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSON line: %v", err)
		}
		summary = fmt.Sprint(event["summary"])
	}
	if strings.Contains(summary, "hunter2-secret") {
		t.Errorf("summary holds a masked value: %q", summary)
	}
}
