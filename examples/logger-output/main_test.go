package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/jeremygprawira/wlog"
	wlogslog "github.com/jeremygprawira/wlog/log/slog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLoggerOutput_EventReachesZapAndSlog proves one event reaches both the zap logger
// and the slog handler.
func TestLoggerOutput_EventReachesZapAndSlog(t *testing.T) {
	core, recorded := observer.New(zap.DebugLevel)
	var buf bytes.Buffer
	logger := newLogger(zap.New(core), slog.NewJSONHandler(&buf, nil))

	ctx := logger.WithContext(context.Background())
	_, end := wlog.Start(ctx, "job.run")
	end()

	entries := recorded.All()
	if len(entries) != 1 || entries[0].Message != "job.run" {
		t.Fatalf("zap entries = %v, want one job.run entry", entries)
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("slog output is not one JSON record: %v\n%q", err, buf.String())
	}
	if rec["msg"] != "job.run" {
		t.Errorf("slog msg = %v, want job.run", rec["msg"])
	}
}

// TestLoggerOutput_SlogInputFoldsIntoEvent proves a slog call made with the event's
// context lands in that event's logs[].
func TestLoggerOutput_SlogInputFoldsIntoEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "op")
	handler := wlogslog.Handler(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	slog.New(handler).InfoContext(ctx, "folded", "k", "v")
	end()

	logs, _ := rec.Last()["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want one folded line", rec.Last()["logs"])
	}
	line, _ := logs[0].(map[string]any)
	if line["msg"] != "folded" {
		t.Errorf("folded msg = %v, want folded", line["msg"])
	}
}
