package wlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// modeRecorder collects events from a drain, so a mode test can see what arrived
// without reading stdout.
type modeRecorder struct{ events []map[string]any }

func (r *modeRecorder) drain() wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		r.events = append(r.events, event)
	})
}

// TestCore_Enabled proves the switch of one Logger makes its own Start a no-op, and that
// turning it back on restores logging for that Logger.
func TestCore_Enabled(t *testing.T) {
	rec := &modeRecorder{}
	log := wlog.New(wlog.WithDrains(rec.drain()))
	ctx := log.WithContext(context.Background())

	log.SetEnabled(false)
	if log.Enabled() {
		t.Fatal("Enabled() = true after SetEnabled(false)")
	}
	out := captureStdout(t, func() {
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if len(rec.events) != 0 || out != "" {
		t.Errorf("disabled logging emitted %d events and wrote %q", len(rec.events), out)
	}

	log.SetEnabled(true)
	captureStdout(t, func() {
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if len(rec.events) != 1 {
		t.Errorf("events after re-enable = %d, want 1", len(rec.events))
	}
}

// TestCore_Silent proves a silent logger writes nothing to stdout and still reaches
// every drain.
func TestCore_Silent(t *testing.T) {
	rec := &modeRecorder{}
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(rec.drain()))
	ctx := log.WithContext(context.Background())

	out := captureStdout(t, func() {
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "order_id", "1")
		end()

		flushWriter(t, log)
	})
	if out != "" {
		t.Errorf("silent logger wrote %q to stdout", out)
	}
	if len(rec.events) != 1 || rec.events[0]["order_id"] != "1" {
		t.Errorf("silent logger events = %v, want one event with order_id", rec.events)
	}
}

// TestCore_RawValues proves a tree-form value passes through raw mode and the redactor
// still walks it.
func TestCore_RawValues(t *testing.T) {
	rec := &modeRecorder{}
	log := wlog.New(wlog.WithRawValues(), wlog.WithDrains(rec.drain()))
	ctx := log.WithContext(context.Background())

	payload := map[string]any{"note": "ok", "password": "hunter2"}
	captureStdout(t, func() {
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "payload", payload)
		end()

		flushWriter(t, log)
	})

	if len(rec.events) != 1 {
		t.Fatalf("events = %d, want 1", len(rec.events))
	}
	stored, _ := rec.events[0]["payload"].(map[string]any)
	if stored["note"] != "ok" {
		t.Errorf("payload.note = %v, want ok", stored["note"])
	}
	if stored["password"] == "hunter2" {
		t.Error("raw mode stored the denied value; the redactor must still walk it")
	}
}
