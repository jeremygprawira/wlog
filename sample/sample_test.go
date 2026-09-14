package sample_test

import (
	"context"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/sample"
)

func TestSample_Default_KeepsEverything(t *testing.T) {
	k := sample.New()
	event := map[string]any{"level": "info"}
	if !k.Keep(context.Background(), event) {
		t.Error("default sampler dropped an event")
	}
}

func TestSample_Rate_ZeroDropsInfo(t *testing.T) {
	k := sample.New(sample.Rate(wlog.LevelInfo, 0))
	event := map[string]any{"level": "info"}
	if k.Keep(context.Background(), event) {
		t.Error("Rate(Info, 0) kept an info event")
	}
}

func TestSample_Rate_HundredKeepsInfo(t *testing.T) {
	k := sample.New(sample.Rate(wlog.LevelInfo, 100))
	event := map[string]any{"level": "info"}
	if !k.Keep(context.Background(), event) {
		t.Error("Rate(Info, 100) dropped an info event")
	}
}

func TestSample_Errors_AlwaysKept_EvenAtZeroRate(t *testing.T) {
	k := sample.New(sample.Rate(wlog.LevelError, 0))
	event := map[string]any{"level": "error"}
	if !k.Keep(context.Background(), event) {
		t.Error("an error-level event was dropped despite Rate(Error, 0)")
	}
}

func TestSample_KeepStatus_OverridesZeroRate(t *testing.T) {
	k := sample.New(sample.Rate(wlog.LevelInfo, 0), sample.KeepStatus(500))
	event := map[string]any{"level": "info", "http": map[string]any{"status": 503}}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepStatus(500) did not force-keep a 503 event")
	}

	okEvent := map[string]any{"level": "info", "http": map[string]any{"status": 200}}
	if k.Keep(context.Background(), okEvent) {
		t.Error("a non-matching status went to head sampling and should have been dropped at rate 0")
	}
}

func TestSample_KeepDuration_OverridesZeroRate(t *testing.T) {
	k := sample.New(sample.Rate(wlog.LevelInfo, 0), sample.KeepDuration(time.Second))
	event := map[string]any{"level": "info", "duration_ms": int64(2000)}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepDuration(1s) did not force-keep a 2s event")
	}
}

func TestSample_KeepPath_OverridesZeroRate(t *testing.T) {
	k := sample.New(sample.Rate(wlog.LevelInfo, 0), sample.KeepPath("/api/payments/**"))
	event := map[string]any{"level": "info", "http": map[string]any{"path": "/api/payments/refund"}}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepPath did not force-keep a matching path")
	}

	other := map[string]any{"level": "info", "http": map[string]any{"path": "/api/orders"}}
	if k.Keep(context.Background(), other) {
		t.Error("a non-matching path went to head sampling and should have been dropped at rate 0")
	}
}

func TestSample_KeepFunc(t *testing.T) {
	k := sample.New(sample.Rate(wlog.LevelInfo, 0), sample.KeepFunc(func(_ context.Context, event map[string]any) bool {
		return event["vip"] == true
	}))
	event := map[string]any{"level": "info", "vip": true}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepFunc did not force-keep a matching event")
	}
}

func TestSample_KeepErrorsAndSlow(t *testing.T) {
	k := sample.New(sample.KeepErrorsAndSlow(time.Second, 0))

	slow := map[string]any{"level": "info", "duration_ms": int64(5000)}
	if !k.Keep(context.Background(), slow) {
		t.Error("KeepErrorsAndSlow did not keep a slow request")
	}

	errEvent := map[string]any{"level": "error", "duration_ms": int64(1)}
	if !k.Keep(context.Background(), errEvent) {
		t.Error("KeepErrorsAndSlow did not keep an error event")
	}

	healthy := map[string]any{"level": "info", "duration_ms": int64(1)}
	if k.Keep(context.Background(), healthy) {
		t.Error("KeepErrorsAndSlow kept a healthy fast event at healthyRate 0")
	}
}
