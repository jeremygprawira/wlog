package wlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog"
)

func TestCore_PlainLog_Info(t *testing.T) {
	log := wlog.New(wlog.WithService("svc", "1.0.0", "test"))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		wlog.Info(ctx, "server started", "port", 8080)
	})

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON line: %v\noutput: %q", err, out)
	}
	if got["kind"] != "log" || got["level"] != "info" || got["message"] != "server started" {
		t.Errorf("got = %v", got)
	}
	if got["port"] != float64(8080) {
		t.Errorf("port = %v, want 8080", got["port"])
	}
}

func TestCore_PlainLog_WarnAndDebug(t *testing.T) {
	log := wlog.New()
	for _, tc := range []struct {
		fn    func(context.Context, string, ...any)
		level string
	}{
		{wlog.Warn, "warn"},
		{wlog.Debug, "debug"},
	} {
		out := captureStdout(t, func() {
			ctx := log.WithContext(context.Background())
			tc.fn(ctx, "msg")
		})
		var got map[string]any
		json.Unmarshal([]byte(out), &got)
		if got["level"] != tc.level {
			t.Errorf("level = %v, want %v", got["level"], tc.level)
		}
	}
}

func TestCore_PlainLog_RedactsSensitiveKV(t *testing.T) {
	log := wlog.New()
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		wlog.Info(ctx, "login attempt", "password", "hunter2")
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED]", got["password"])
	}
}

func TestCore_PlainLog_StandaloneInsideRequest(t *testing.T) {
	var outputs []string
	log := wlog.New(wlog.WithDrains(wlog.DrainFunc(func(_ context.Context, e map[string]any) {
		if e["kind"] == "log" {
			outputs = append(outputs, e["message"].(string))
		}
	})))

	captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "request.op")
		wlog.Info(ctx, "inside a request")
		end()
	})

	if len(outputs) != 1 || outputs[0] != "inside a request" {
		t.Errorf("plain log inside a request was not emitted standalone: %v", outputs)
	}
}
