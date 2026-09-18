package wlog_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what was
// written to it. A test that reads the output also calls flushWriter inside fn, because
// the default writer queues its line and writes it on another goroutine.
//
// It redirects to a file, not a pipe, because an event larger than the pipe buffer
// would otherwise block the writer for good.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	orig := os.Stdout
	os.Stdout = f
	fn()
	os.Stdout = orig

	out, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(out)
}

// flushWriter waits until log's writer has written every line it queued, so a test reads
// the output of the async writer.
func flushWriter(t *testing.T, log *wlog.Logger) {
	t.Helper()
	if err := log.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestCore_StartSetEmit(t *testing.T) {
	log := wlog.New(wlog.WithService("go-customer", "1.4.0", "test"))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "order.create")
		wlog.Set(ctx, "order_id", "4821")
		wlog.Set(ctx, "password", "hunter2")
		end()

		flushWriter(t, log)
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

// TestCore_Start_WithoutLogger_UsesDefault proves that a bare context starts an event
// through the default Logger, so a package function works with no setup.
func TestCore_Start_WithoutLogger_UsesDefault(t *testing.T) {
	out := captureStdout(t, func() {
		ctx, end := wlog.Start(context.Background(), "no-setup.op")
		wlog.Set(ctx, "key", "value")
		end()
		if err := wlog.Default().Flush(context.Background()); err != nil {
			t.Fatalf("Flush: %v", err)
		}
	})
	if !strings.Contains(out, "no-setup.op") {
		t.Errorf("Start with no Logger on the context wrote %q, want one line through Default", out)
	}
}
