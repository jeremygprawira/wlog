// This file tests the two sampling decisions of package sample: Sample draws the head
// rate of one level, and Keep force-keeps the enriched events a team needs. The tests
// build their own read-only event, so a rule is tested without a logger.
package sample_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/sample"
)

// mapEvent is a read-only event built from a map, so a test calls Keep without a
// logger. Its level comes from the level field, which is how core names it.
type mapEvent map[string]any

// Get reads a value at a dotted path, such as "http.status".
func (e mapEvent) Get(path string) (any, bool) {
	var current any = map[string]any(e)
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// Kind returns no kind, because a rule never reads it.
func (e mapEvent) Kind() string { return "" }

// Level reads the level field of the event.
func (e mapEvent) Level() wlog.Level {
	level, _ := e["level"].(string)
	return wlog.Level(level)
}

func TestSample_Default_KeepsEverything(t *testing.T) {
	k := sample.MustNew()
	if keep, rate := k.Sample(wlog.LevelInfo, ""); !keep || rate != 100 {
		t.Errorf("default sampler = (%v, %v), want every event kept at 100", keep, rate)
	}
}

func TestSample_Rate_ZeroDropsInfo(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0))
	if keep, _ := k.Sample(wlog.LevelInfo, ""); keep {
		t.Error("Rate(Info, 0) kept an info event")
	}
}

func TestSample_Rate_HundredKeepsInfo(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 100))
	if keep, _ := k.Sample(wlog.LevelInfo, ""); !keep {
		t.Error("Rate(Info, 100) dropped an info event")
	}
}

func TestSample_Errors_AlwaysKept_EvenAtZeroRate(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelError, 0))
	if !k.Keep(context.Background(), mapEvent{"level": "error"}) {
		t.Error("an error-level event was dropped despite Rate(Error, 0)")
	}
}

func TestSample_KeepStatus_OverridesZeroRate(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepStatus(500))
	event := mapEvent{"level": "info", "http": map[string]any{"status": 503}}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepStatus(500) did not force-keep a 503 event")
	}

	okEvent := mapEvent{"level": "info", "http": map[string]any{"status": 200}}
	if k.Keep(context.Background(), okEvent) {
		t.Error("Keep kept a healthy event, which is the head decision's job")
	}
}

func TestSample_KeepDuration_OverridesZeroRate(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepDuration(time.Second))
	event := mapEvent{"level": "info", "duration_ms": int64(2000)}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepDuration(1s) did not force-keep a 2s event")
	}
}

func TestSample_KeepPath_OverridesZeroRate(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepPath("/api/payments/**"))
	event := mapEvent{"level": "info", "http": map[string]any{"path": "/api/payments/refund"}}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepPath did not force-keep a matching path")
	}

	other := mapEvent{"level": "info", "http": map[string]any{"path": "/api/orders"}}
	if k.Keep(context.Background(), other) {
		t.Error("Keep kept a path that matches no rule")
	}
}

func TestSample_KeepFunc(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepFunc(func(_ context.Context, event wlog.Event) bool {
		vip, ok := event.Get("vip")
		return ok && vip == true
	}))
	event := mapEvent{"level": "info", "vip": true}
	if !k.Keep(context.Background(), event) {
		t.Error("KeepFunc did not force-keep a matching event")
	}
}

func TestSample_KeepErrorsAndSlow(t *testing.T) {
	k := sample.MustNew(sample.KeepErrorsAndSlow(time.Second, 0))

	slow := mapEvent{"level": "info", "duration_ms": int64(5000)}
	if !k.Keep(context.Background(), slow) {
		t.Error("KeepErrorsAndSlow did not keep a slow request")
	}

	errEvent := mapEvent{"level": "error", "duration_ms": int64(1)}
	if !k.Keep(context.Background(), errEvent) {
		t.Error("KeepErrorsAndSlow did not keep an error event")
	}

	healthy := mapEvent{"level": "info", "duration_ms": int64(1)}
	if k.Keep(context.Background(), healthy) {
		t.Error("KeepErrorsAndSlow kept a healthy fast event")
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
		event := mapEvent{"level": "info", "http": map[string]any{"path": tc.path}}
		if got := k.Keep(context.Background(), event); got != tc.keep {
			t.Errorf("Keep(%q) = %v, want %v", tc.path, got, tc.keep)
		}
	}

	// One star stays inside one segment.
	single := sample.MustNew(sample.Rate(wlog.LevelInfo, 0), sample.KeepPath("/api/*"))
	if single.Keep(context.Background(), mapEvent{"level": "info", "http": map[string]any{"path": "/api/payments/123"}}) {
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
	const id = "4bf92f3577b34da6a3ce929d0e0e4736"

	first, rate := k.Sample(wlog.LevelInfo, id)
	if rate != 12.5 {
		t.Errorf("rate = %v, want the configured 12.5", rate)
	}
	for i := 0; i < 20; i++ {
		// Same trace id, same answer, however many times it is asked.
		if got, _ := k.Sample(wlog.LevelInfo, id); got != first {
			t.Fatalf("the same trace id kept %v then %v", first, got)
		}
	}

	// A rate of 50 over many trace ids keeps roughly half of them.
	half := sample.MustNew(sample.Rate(wlog.LevelInfo, 50))
	kept := 0
	for i := 0; i < 400; i++ {
		if keep, _ := half.Sample(wlog.LevelInfo, fmt.Sprintf("trace-%d", i)); keep {
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

// TestSample_SMP4_RateReported proves the head decision reports the rate that decided it,
// so core can record the rate on the event and a reader can weigh a sampled count.
func TestSample_SMP4_RateReported(t *testing.T) {
	k := sample.MustNew(sample.Rate(wlog.LevelInfo, 25))
	if _, rate := k.Sample(wlog.LevelInfo, "any-trace"); rate != 25 {
		t.Errorf("rate = %v, want the configured 25", rate)
	}

	// A level with no Rate call is kept whole, at rate 100.
	if keep, rate := k.Sample(wlog.LevelError, ""); !keep || rate != 100 {
		t.Errorf("an error level = (%v, %v), want (true, 100)", keep, rate)
	}
}

// TestSample_SMP2_KeepsWarnAnd5xx proves the production preset keeps warn and a 5xx, and
// never keeps a healthy debug line, instead of sampling a warn away.
func TestSample_SMP2_KeepsWarnAnd5xx(t *testing.T) {
	k := sample.MustNew(sample.KeepErrorsAndSlow(time.Minute, 1))

	kept := []mapEvent{
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
		if keep, _ := k.Sample(wlog.LevelInfo, fmt.Sprintf("t-%d", i)); keep {
			infoKept++
		}
		if keep, _ := k.Sample(wlog.LevelDebug, fmt.Sprintf("d-%d", i)); keep {
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
