package wlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func TestCore_Level_DefaultInfo(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.Set(ctx, "x", 1)
	got := finish()

	if got["level"] != "info" {
		t.Errorf("level = %v, want info", got["level"])
	}
	if got["outcome"] != "success" {
		t.Errorf("outcome = %v, want success", got["outcome"])
	}
}

func TestCore_SetLevel_OverridesDefault(t *testing.T) {
	ctx, finish := startEvent(t)
	wlog.SetLevel(ctx, wlog.LevelError)
	got := finish()

	if got["level"] != "error" {
		t.Errorf("level = %v, want error", got["level"])
	}
	if got["outcome"] != "error" {
		t.Errorf("outcome = %v, want error", got["outcome"])
	}
}

func TestCore_WithLevel_FiltersBelowMinimum(t *testing.T) {
	log := wlog.New(wlog.WithLevel(wlog.LevelWarn))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "quiet.op")
		wlog.Set(ctx, "x", 1) // stays at the default info level
		end()

		flushWriter(t, log)
	})
	if out != "" {
		t.Errorf("expected no output below the minimum level, got %q", out)
	}
}

func TestCore_WithLevel_KeepsAtOrAboveMinimum(t *testing.T) {
	log := wlog.New(wlog.WithLevel(wlog.LevelWarn))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "loud.op")
		wlog.SetLevel(ctx, wlog.LevelError)
		end()

		flushWriter(t, log)
	})
	if out == "" {
		t.Error("expected output at or above the minimum level, got none")
	}
}

// TestCore_CORE9_AuditSkipsLevelFilter proves that an event carrying an audit
// record reaches its drains even when the minimum level is above the event's own
// level, because an audit fact is never sampled or filtered away.
func TestCore_CORE9_AuditSkipsLevelFilter(t *testing.T) {
	for _, tc := range []struct {
		name string
		log  *wlog.Logger
	}{
		{"WithLevel(LevelError)", nil},
		{"WLOG_LEVEL=error", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WLOG_LEVEL", "")
			opts := []wlog.Option{wlog.WithLevel(wlog.LevelError)}
			if tc.name == "WLOG_LEVEL=error" {
				t.Setenv("WLOG_LEVEL", "error")
				opts = nil
			}

			log, rec := wlogtest.New(t, opts...)
			ctx := log.WithContext(context.Background())

			ctx, end := wlog.Start(ctx, "refund")
			wlog.Set(ctx, "audit", map[string]any{"action": "invoice.refund"})
			wlog.Set(ctx, "note", "kept")
			end()

			if rec.Count() != 1 {
				t.Fatalf("the audit event was filtered out: %d events", rec.Count())
			}
			if _, ok := rec.Last()["audit"]; !ok {
				t.Errorf("the drained event lost its audit field: %v", rec.Last())
			}

			// An ordinary event at the same level is still filtered.
			ctx2, end2 := wlog.Start(ctx, "quiet")
			wlog.Set(ctx2, "note", "filtered")
			end2()
			if rec.Count() != 1 {
				t.Errorf("an ordinary event passed the level filter: %v", rec.Events())
			}
		})
	}
}
