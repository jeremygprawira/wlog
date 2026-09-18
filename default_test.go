// This file tests the default Logger and the per-Logger on switch. A package function
// with no Logger on the context uses Default, and one Logger that is off leaves every
// other Logger on.
package wlog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestDefault_PAR5_InfoWithoutSetup proves that wlog logs with no setup at all: a package
// function with an empty context writes through Default, and SetDefault replaces the
// Logger that those package functions use.
func TestDefault_PAR5_InfoWithoutSetup(t *testing.T) {
	out := captureStdout(t, func() {
		wlog.Info(context.Background(), "started")
		if err := wlog.Default().Flush(context.Background()); err != nil {
			t.Fatalf("Flush: %v", err)
		}
	})
	if !strings.Contains(out, `"message":"started"`) {
		t.Errorf("Info with no setup wrote %q, want one line through Default", out)
	}

	// SetDefault sends the same call to another Logger.
	previous := wlog.Default()
	log, rec := wlogtest.New(t)
	wlog.SetDefault(log)
	t.Cleanup(func() { wlog.SetDefault(previous) })

	wlog.Info(context.Background(), "second")
	if rec.Count() != 1 {
		t.Errorf("the default Logger received %d events, want 1", rec.Count())
	}
}

// TestDefault_CORE19_EnabledPerLogger proves the on switch belongs to one Logger: turning
// one off leaves another on, so two tests or two services never interfere.
func TestDefault_CORE19_EnabledPerLogger(t *testing.T) {
	on, onRec := wlogtest.New(t)
	off, offRec := wlogtest.New(t)

	off.SetEnabled(false)
	if off.Enabled() {
		t.Error("Enabled() = true after SetEnabled(false)")
	}
	if !on.Enabled() {
		t.Error("Enabled() = false on a Logger that was never switched off")
	}

	_, endOn := wlog.Start(on.WithContext(context.Background()), "op")
	endOn()
	_, endOff := wlog.Start(off.WithContext(context.Background()), "op")
	endOff()

	if onRec.Count() != 1 {
		t.Errorf("the Logger that is on received %d events, want 1", onRec.Count())
	}
	if offRec.Count() != 0 {
		t.Errorf("the Logger that is off received %d events, want 0", offRec.Count())
	}
}

// sink is a drain that keeps nothing, so a test reads only the problems it asks about.
var sink = wlog.DrainFunc(func(context.Context, map[string]any) {})

// TestDefault_CORE18_OrphanSetReported proves that a write with no event on the context
// reports WLOG_NO_EVENT with the key name, so a silent drop becomes visible.
func TestDefault_CORE18_OrphanSetReported(t *testing.T) {
	problems := make(chan wlog.Problem, 8)
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithDrains(sink),
		wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
	)
	// The context carries a Logger, and no event, which is a write outside Start.
	ctx := log.WithContext(context.Background())

	wlog.Set(ctx, "order_id", "1")
	wlog.SetGroup(ctx, "http", "status", 200)
	wlog.Append(ctx, "tags", "one")
	wlog.SetLevel(ctx, wlog.LevelWarn)
	wlog.AppendLog(ctx, wlog.LogLine{Level: "info", Msg: "folded"})

	close(problems)
	sources := make([]string, 0, 5)
	for p := range problems {
		if p.Code != "WLOG_NO_EVENT" {
			t.Errorf("code = %q, want WLOG_NO_EVENT", p.Code)
		}
		sources = append(sources, p.Source)
	}
	want := []string{"order_id", "http", "tags", "level", "logs"}
	if len(sources) != len(want) {
		t.Fatalf("reports = %v, want one per write: %v", sources, want)
	}
	for i, source := range want {
		if sources[i] != source {
			t.Errorf("report %d names %q, want %q", i, sources[i], source)
		}
	}
}

// TestDefault_CORE18_OrphanErrorEmits proves that an error with no event still reaches a
// reader: one log event at level error, carrying the ErrorInfo.
func TestDefault_CORE18_OrphanErrorEmits(t *testing.T) {
	problems := make(chan wlog.Problem, 4)
	log, rec := wlogtest.New(t,
		wlog.WithErrorExtractor(customExtractor{}),
		wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
	)
	ctx := log.WithContext(context.Background())

	wlog.Error(ctx, errAny{})

	if rec.Count() != 1 {
		t.Fatalf("events = %d, want one log event for an error with no event", rec.Count())
	}
	got := rec.Last()
	if got["kind"] != "log" || got["level"] != "error" {
		t.Errorf("kind/level = %v/%v, want a log event at level error", got["kind"], got["level"])
	}
	errInfo, _ := got["error"].(map[string]any)
	if errInfo["code"] != "ORDER_NOT_FOUND" {
		t.Errorf("error.code = %v, want the extracted code", errInfo["code"])
	}

	select {
	case p := <-problems:
		if p.Code != "WLOG_NO_EVENT" {
			t.Errorf("code = %q, want WLOG_NO_EVENT", p.Code)
		}
	default:
		t.Error("an error with no event reported nothing")
	}
}

// TestDefault_SPECG14_LogAnyLevel proves Log writes a line at any level, including one
// below the minimum level of the Logger, and that Info stays filtered.
func TestDefault_SPECG14_LogAnyLevel(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithLevel(wlog.LevelError))
	ctx := log.WithContext(context.Background())

	wlog.Info(ctx, "below the minimum")
	wlog.Log(ctx, wlog.LevelDebug, "forced through")
	wlog.Log(ctx, wlog.LevelError, "a failure")

	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d, want the two Log lines and no Info line", len(events))
	}
	if events[0]["message"] != "forced through" || events[0]["level"] != "debug" {
		t.Errorf("first line = %v/%v, want the debug line", events[0]["message"], events[0]["level"])
	}
	if events[1]["level"] != "error" {
		t.Errorf("second line level = %v, want error", events[1]["level"])
	}
}
