// This file checks the standard library log bridge: a per-unit logger folds each line
// into the event of its context, and the server error logger writes plain events.
package wlogstdlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogstdlog "github.com/jeremygprawira/wlog/log/std"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestStdLog_C2_FoldsLine proves that each line of the per-unit logger lands in the logs
// array of the open event.
func TestStdLog_C2_FoldsLine(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	logger := wlogstdlog.Logger(ctx, "", 0)
	logger.Print("the first line")
	logger.Print("the second line")
	end()

	logs, _ := rec.Last()["logs"].([]any)
	if len(logs) != 2 {
		t.Fatalf("logs = %d, want 2", len(logs))
	}
	for i, want := range []string{"the first line", "the second line"} {
		line, _ := logs[i].(map[string]any)
		if line["msg"] != want {
			t.Errorf("logs[%d].msg = %v, want %q", i, line["msg"], want)
		}
		if line["level"] != "info" {
			t.Errorf("logs[%d].level = %v, want info", i, line["level"])
		}
	}
}

// TestStdLog_C2_Prefix proves that the prefix of the stdlib logger stays on the folded
// message.
func TestStdLog_C2_Prefix(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	wlogstdlog.Logger(ctx, "app: ", 0).Print("ready")
	end()

	logs, _ := rec.Last()["logs"].([]any)
	line, _ := logs[0].(map[string]any)
	if line["msg"] != "app: ready" {
		t.Errorf("logs[0].msg = %v, want the prefix and the text", line["msg"])
	}
}

// TestStdLog_C2_ErrorLogPlainEvent proves that the server error logger writes one plain
// event at level error, because net/http writes it with no context.
func TestStdLog_C2_ErrorLogPlainEvent(t *testing.T) {
	log, rec := wlogtest.New(t)

	wlogstdlog.ErrorLog(log).Print("the server failed")

	if rec.Count() != 1 {
		t.Fatalf("events = %d, want 1", rec.Count())
	}
	event := rec.Last()
	if event["kind"] != "log" {
		t.Errorf("kind = %v, want log", event["kind"])
	}
	if event["level"] != "error" {
		t.Errorf("level = %v, want error", event["level"])
	}
	if event["message"] != "the server failed" {
		t.Errorf("message = %v, want the line", event["message"])
	}
}

// TestStdLog_C2_NoEventReports proves that a line with no event reports a problem instead
// of panicking, which is the core rule for a write with no event.
func TestStdLog_C2_NoEventReports(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	ctx := rec.Logger().WithContext(context.Background())

	wlogstdlog.Logger(ctx, "", 0).Print("orphan")

	if count := len(rec.Problems()); count != 1 {
		t.Errorf("problems = %d, want 1", count)
	}
}
