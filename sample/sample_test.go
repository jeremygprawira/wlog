package sample_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/sample"
)

func TestSample_Default_KeepsEverything(t *testing.T) {
	k := sample.MustNew()
	event := map[string]any{"level": "info"}
	if !k.Keep(context.Background(), event) {
		t.Error("default sampler dropped an event")
	}
}

func TestSample_Rate_ZeroDropsInfo(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0))
	event := map[string]any{"level": "info"}
	if k.Keep(context.Background(), event) {
		t.Error("Rate(Info, 0) kept an info event")
	}
}

func TestSample_Rate_HundredKeepsInfo(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 100))
	event := map[string]any{"level": "info"}
	if !k.Keep(context.Background(), event) {
		t.Error("Rate(Info, 100) dropped an info event")
	}
}

func TestSample_Errors_AlwaysKept_EvenAtZeroRate(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelError, 0))
	event := map[string]any{"level": "error"}
	if !k.Keep(context.Background(), event) {
		t.Error("an error-level event was dropped despite Rate(Error, 0)")
	}
}

func TestSample_KeepStatus_OverridesZeroRate(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepStatus(500))
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
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepDuration(time.Second))
	event := map[string]any{"level": "info", "duration_ms": int64(2000)}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepDuration(1s) did not force-keep a 2s event")
	}
}

func TestSample_KeepPath_OverridesZeroRate(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepPath("/api/payments/**"))
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
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepFunc(func(_ context.Context, event map[string]any) bool {
		return event["vip"] == true
	}))
	event := map[string]any{"level": "info", "vip": true}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepFunc did not force-keep a matching event")
	}
}

func TestSample_KeepErrorsAndSlow(t *testing.T) {
	k := sample.MustNew(sample.KeepErrorsAndSlow(time.Second, 0))

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

// TestSample_SMP3_DoubleStarGlob proves ** crosses segments, * does not, and a glob that can
// never match is refused at construction instead of silently keeping everything.
func TestSample_SMP3_DoubleStarGlob(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepPath("/api/**"))
	cases := []struct {
		path string
		keep bool
	}{
		{"/api", true},              // ** matches no segment
		{"/api/payments/123", true}, // and any run of segments
		{"/other/api", false},
	}
	for _, tc := range cases {
		event := map[string]any{"level": "info", "http": map[string]any{"path": tc.path}}
		if got := k.Keep(context.Background(), event); got != tc.keep {
			t.Errorf("Keep(%q) = %v, want %v", tc.path, got, tc.keep)
		}
	}

	// One star stays inside one segment.
	single := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepPath("/api/*"))
	if single.Keep(context.Background(), map[string]any{"level": "info", "http": map[string]any{"path": "/api/payments/123"}}) {
		t.Error("* crossed a slash, which it must not")
	}

	// A glob that cannot match, and one that mixes ** with other characters, are errors.
	if _, err := sample.New(sample.KeepPath("/api/[")); err == nil {
		t.Error("New accepted an invalid glob")
	}
	if _, err := sample.New(sample.KeepPath("/api/**/x**")); err == nil {
		t.Error("New accepted ** beside other characters")
	}
}

// TestSample_SMP4_TraceConsistent proves the head decision follows the trace id, so every
// service and every retry of the same request agrees, and that a fractional rate is used.
func TestSample_SMP4_TraceConsistent(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 12.5))
	event := map[string]any{
		"level": "info",
		"trace": map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"},
	}

	first := k.Keep(context.Background(), event)
	for i := 0; i < 20; i++ {
		// Same trace id, same answer, however many times it is asked.
		fresh := map[string]any{
			"level": "info",
			"trace": map[string]any{"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736"},
		}
		if got := k.Keep(context.Background(), fresh); got != first {
			t.Fatalf("the same trace id kept %v then %v", first, got)
		}
	}

	// A rate of 50 over many trace ids keeps roughly half of them.
	half := sample.MustNew(sample.Rate(wlog.LevelInfo, 50))
	kept := 0
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("trace-%d", i)
		if half.Keep(context.Background(), map[string]any{"level": "info", "trace": map[string]any{"trace_id": id}}) {
			kept++
		}
	}
	if kept < 120 || kept > 280 {
		t.Errorf("a 50%% rate kept %d of 400 trace ids, want roughly half", kept)
	}

	// A rate outside 0 to 100 is refused.
	if _, err := sample.New(sample.Rate(wlog.LevelInfo, 101)); err == nil {
		t.Error("New accepted a rate above 100")
	}
	if _, err := sample.New(sample.Rate(wlog.LevelInfo, -1)); err == nil {
		t.Error("New accepted a negative rate")
	}
}

// TestSample_SMP4_RateRecorded proves a kept event records the rate that kept it, so a
// reader can weigh a sampled count.
func TestSample_SMP4_RateRecorded(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 25))
	event := map[string]any{"level": "info", "trace": map[string]any{"trace_id": "keep-me-25"}}
	if !k.Keep(context.Background(), event) {
		t.Fatalf("no trace id was kept at 25%%; the test needs one")
	}
	if got, _ := event["wlog.sample_rate"].(float64); got != 25 {
		t.Errorf("wlog.sample_rate = %v, want 25", event["wlog.sample_rate"])
	}

	// A tail-kept event records 100, because nothing sampled it away.
	tail := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepStatus(500))
	tailed := map[string]any{"level": "info", "http": map[string]any{"status": 503}}
	if !tail.Keep(context.Background(), tailed) {
		t.Fatal("KeepStatus did not keep a 503")
	}
	if got, _ := tailed["wlog.sample_rate"].(float64); got != 100 {
		t.Errorf("wlog.sample_rate = %v, want 100 for a tail-kept event", tailed["wlog.sample_rate"])
	}

	// An error event is always kept, and records 100 as well.
	errEvent := map[string]any{"level": "error"}
	if !k.Keep(context.Background(), errEvent) {
		t.Fatal("an error event was dropped")
	}
	if got, _ := errEvent["wlog.sample_rate"].(float64); got != 100 {
		t.Errorf("wlog.sample_rate = %v, want 100 for an error", errEvent["wlog.sample_rate"])
	}

	// A dropped event carries nothing.
	dropped := map[string]any{"level": "info", "trace": map[string]any{"trace_id": "drop-me"}}
	if k.Keep(context.Background(), dropped) {
		t.Log("this trace id was kept, so the drop path is covered by another draw")
	} else if _, ok := dropped["wlog.sample_rate"]; ok {
		t.Error("a dropped event recorded a rate")
	}
}

// TestSample_SMP2_KeepsWarnAnd5xx proves the production preset keeps warn and a 5xx, and
// never keeps a healthy debug line, instead of sampling a warn away.
func TestSample_SMP2_KeepsWarnAnd5xx(t *testing.T) {
	k := sample.MustNew(sample.KeepErrorsAndSlow(time.Minute, 1))

	kept := []map[string]any{
		{"level": "warn"},
		// A 5xx with no wlog.Error must still be kept: the status is the failure.
		{"level": "info", "http": map[string]any{"status": 503}},
		{"level": "error"},
		{"level": "info", "duration_ms": 90_000},
	}
	for _, event := range kept {
		if !k.Keep(context.Background(), event) {
			t.Errorf("KeepErrorsAndSlow dropped %v", event)
		}
	}

	// A healthy info line is sampled at the healthy rate, and a healthy debug line is not
	// kept at all.
	infoKept, debugKept := 0, 0
	for i := 0; i < 400; i++ {
		trace := map[string]any{"trace_id": fmt.Sprintf("t-%d", i)}
		event := map[string]any{"level": "info", "http": map[string]any{"status": 200}, "trace": trace}
		if k.Keep(context.Background(), event) {
			infoKept++
		}
		debug := map[string]any{"level": "debug", "trace": map[string]any{"trace_id": fmt.Sprintf("d-%d", i)}}
		if k.Keep(context.Background(), debug) {
			debugKept++
		}
	}
	if infoKept > 40 {
		t.Errorf("a 1%% healthy rate kept %d of 400 info events", infoKept)
	}
	if debugKept != 0 {
		t.Errorf("a healthy debug line was kept %d times", debugKept)
	}
}
