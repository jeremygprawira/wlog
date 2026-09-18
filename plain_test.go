package wlog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func TestCore_PlainLog_Info(t *testing.T) {
	log := wlog.New(wlog.WithService("svc", "1.0.0", "test"))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		wlog.Info(ctx, "server started", "port", 8080)

		flushWriter(t, log)
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

			flushWriter(t, log)
		})
		var got map[string]any
		_ = json.Unmarshal([]byte(out), &got)
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

		flushWriter(t, log)
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
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

		flushWriter(t, log)
	})

	if len(outputs) != 1 || outputs[0] != "inside a request" {
		t.Errorf("plain log inside a request was not emitted standalone: %v", outputs)
	}
}

// boxed is a caller type whose secret is named only by a json tag, so a plain
// line has to copy it before the redactor can mask it.
type boxed struct {
	Password string `json:"password"`
}

// TestCore_CORE1_PlainLineStructRedacted proves that Info, Warn, and Debug copy
// their key-value pairs the same way Set does, so a struct is walked by its json
// tags and a caller's map is never stored.
func TestCore_CORE1_PlainLineStructRedacted(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	meta := map[string]any{"password": "hunter2"}
	wlog.Info(ctx, "started", "box", boxed{Password: "hunter2"}, "meta", meta)
	wlog.Warn(ctx, "slow", "box", boxed{Password: "hunter2"})
	wlog.Debug(ctx, "detail", "box", boxed{Password: "hunter2"})

	meta["password"] = "changed-after-the-write"

	for _, ev := range rec.Events() {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(line), "hunter2") {
			t.Errorf("a plain line leaked a secret: %s", line)
		}
		if strings.Contains(string(line), "changed-after-the-write") {
			t.Errorf("a plain line stored a caller's map: %s", line)
		}
	}
	if len(rec.Events()) != 3 {
		t.Fatalf("recorded %d plain lines, want 3", len(rec.Events()))
	}
}
