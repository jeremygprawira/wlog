package wlogslog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogslog "github.com/jeremygprawira/wlog/log/slog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// captureDrain wires a wlogtest logger whose events also drain through an slog JSON
// handler into buf, then returns buf's parsed last record.
func captureDrain(t *testing.T, emit func(ctx context.Context)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	log, _ := wlogtest.New(t, wlog.WithDrains(wlogslog.Drain(handler)))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	emit(ctx)
	end()

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("slog output is not one JSON record: %v\n%q", err, buf.String())
	}
	return rec
}

// TestSlogOutput_OneRecordWithNestedGroup proves one wlog event becomes one slog record,
// message from operation, level mapped, and a nested map kept as a slog group.
func TestSlogOutput_OneRecordWithNestedGroup(t *testing.T) {
	rec := captureDrain(t, func(ctx context.Context) {
		wlog.SetGroup(ctx, "http", "status", 200, "method", "GET")
		wlog.Set(ctx, "trace.id", "abc")
	})

	if rec["msg"] != "op" {
		t.Errorf("msg = %v, want op", rec["msg"])
	}
	if rec["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", rec["level"])
	}
	http, _ := rec["http"].(map[string]any)
	if http == nil || http["status"] != float64(200) {
		t.Errorf("http group = %v, want status 200 nested", rec["http"])
	}
	if rec["trace.id"] != "abc" {
		t.Errorf("trace.id = %v, want abc", rec["trace.id"])
	}
}

// TestSlogOutput_LevelMapping proves each wlog level maps to the matching slog level.
func TestSlogOutput_LevelMapping(t *testing.T) {
	cases := map[wlog.Level]string{
		wlog.LevelDebug: "DEBUG",
		wlog.LevelInfo:  "INFO",
		wlog.LevelWarn:  "WARN",
		wlog.LevelError: "ERROR",
	}
	for in, want := range cases {
		rec := captureDrain(t, func(ctx context.Context) { wlog.SetLevel(ctx, in) })
		if rec["level"] != want {
			t.Errorf("wlog level %v mapped to %v, want %v", in, rec["level"], want)
		}
	}
}
