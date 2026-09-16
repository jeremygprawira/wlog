package wlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAppendLog_FoldsAndCaps proves a log-*-in adapter can fold records into logs[]
// and that the array stays bounded at SPEC-core's maxLogLines (50), with the overflow
// counted in wlog.dropped_logs rather than growing the event without limit (G4).
func TestAppendLog_FoldsAndCaps(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	for i := 0; i < 60; i++ {
		wlog.AppendLog(ctx, wlog.LogLine{Level: "info", Msg: "line", Attrs: map[string]any{"i": i}})
	}
	end()

	last := rec.Last()
	logs, ok := last["logs"].([]any)
	if !ok {
		t.Fatalf("logs = %v (%T), want an array", last["logs"], last["logs"])
	}
	if len(logs) != 50 {
		t.Errorf("len(logs) = %d, want 50", len(logs))
	}
	if got := last["wlog.dropped_logs"]; got != int64(10) && got != 10 {
		t.Errorf("wlog.dropped_logs = %v (%T), want 10", got, got)
	}
	first, _ := logs[0].(map[string]any)
	if first["msg"] != "line" || first["level"] != "info" {
		t.Errorf("first log line = %v, want level=info msg=line", first)
	}
}

// TestAppendLog_NoEvent proves a call outside any event is a safe no-op, never a panic
// (G3): adapters are wired in globally, so they run even before Start.
func TestAppendLog_NoEvent(t *testing.T) {
	wlog.AppendLog(context.Background(), wlog.LogLine{Level: "info", Msg: "ignored"})
}
