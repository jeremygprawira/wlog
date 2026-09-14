package wlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
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
	})
	if out == "" {
		t.Error("expected output at or above the minimum level, got none")
	}
}
