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
