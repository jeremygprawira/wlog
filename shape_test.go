// This file tests the time semantics of an event and the fields of ErrorInfo, which
// phase 11 defines: the timestamp is the start of the work, the duration is a float
// with microsecond precision, and an error names its Go type, its causes, and the line
// that recorded it.
package wlog_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
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
