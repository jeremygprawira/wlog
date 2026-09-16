package wlog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written to it. wlog's stdout sink has no other hook point yet (C13 adds Sink), so
// this is the only way to observe it end to end.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	return buf.String()
}

func TestCore_StartSetEmit(t *testing.T) {
	log := wlog.New(wlog.WithService("go-customer", "1.4.0", "test"))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "order.create")
		wlog.Set(ctx, "order_id", "4821")
		wlog.Set(ctx, "password", "hunter2")
		end()
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not one JSON line: %v\noutput: %q", err, out)
	}

	if got["operation"] != "order.create" {
		t.Errorf("operation = %v, want order.create", got["operation"])
	}
	if got["level"] != "info" {
		t.Errorf("level = %v, want info", got["level"])
	}
	if _, ok := got["timestamp"]; !ok {
		t.Error("timestamp missing")
	}
	if _, ok := got["duration_ms"]; !ok {
		t.Error("duration_ms missing")
	}
	svc, ok := got["service"].(map[string]any)
	if !ok || svc["name"] != "go-customer" || svc["version"] != "1.4.0" || svc["env"] != "test" {
		t.Errorf("service = %v, want {go-customer 1.4.0 test}", got["service"])
	}
	if got["order_id"] != "4821" {
		t.Errorf("order_id = %v, want 4821", got["order_id"])
	}
	if got["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED] (default redactor)", got["password"])
	}
}

func TestCore_Set_WithoutEvent_IsNoop(t *testing.T) {
	// No Start was called on this ctx, so there is no event to attach to. Set must
	// not panic and must not write anything.
	wlog.Set(context.Background(), "key", "value")
}

func TestCore_Start_WithoutLogger_IsNoop(t *testing.T) {
	// ctx carries no *Logger (never went through WithContext). Start/end must be
	// harmless no-ops rather than panicking.
	ctx, end := wlog.Start(context.Background(), "op")
	wlog.Set(ctx, "key", "value")
	end()
}
