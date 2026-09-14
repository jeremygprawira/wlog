// Package wlogtest lets a test assert on what its own code logged, without parsing
// JSON off stdout: New builds a real *wlog.Logger backed by an in-memory drain, and
// Recorder reads back what it recorded.
package wlogtest

import (
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
)

// Recorder reads back the events a wlogtest-built Logger has emitted.
type Recorder struct {
	mem *memory.Memory
}

// New builds a *wlog.Logger with JSON output (silencing the pretty console) and an
// in-memory drain, then applies any user opts on top — so a custom redactor, sampler,
// or anything else under test still takes effect.
func New(t testing.TB, opts ...wlog.Option) (*wlog.Logger, *Recorder) {
	t.Helper()
	mem := memory.New(0)
	all := append([]wlog.Option{wlog.WithFormat(wlog.FormatJSON), wlog.WithDrains(mem)}, opts...)
	return wlog.New(all...), &Recorder{mem: mem}
}

// Events returns every event recorded so far, oldest first.
func (r *Recorder) Events() []map[string]any { return r.mem.Snapshot() }

// Count returns how many events have been recorded.
func (r *Recorder) Count() int { return len(r.mem.Snapshot()) }

// Last returns the most recently recorded event, or nil if none has been recorded yet.
func (r *Recorder) Last() map[string]any {
	events := r.mem.Snapshot()
	if len(events) == 0 {
		return nil
	}
	return events[len(events)-1]
}

// RequireField fails the test unless the last event's key equals want.
func (r *Recorder) RequireField(t testing.TB, key string, want any) {
	t.Helper()
	last := r.Last()
	if last == nil {
		t.Fatalf("wlogtest: no event recorded; wanted %s=%v", key, want)
	}
	if got := last[key]; got != want {
		t.Fatalf("wlogtest: %s = %v, want %v\nlast event: %v", key, got, want, last)
	}
}

// RequireErrorCode fails the test unless the last event's error.code equals code.
func (r *Recorder) RequireErrorCode(t testing.TB, code string) {
	t.Helper()
	last := r.Last()
	if last == nil {
		t.Fatalf("wlogtest: no event recorded; wanted error code %s", code)
	}
	errInfo, ok := last["error"].(map[string]any)
	if !ok {
		t.Fatalf("wlogtest: last event has no error; wanted code %s\nlast event: %v", code, last)
	}
	if errInfo["code"] != code {
		t.Fatalf("wlogtest: error.code = %v, want %v\nlast event: %v", errInfo["code"], code, last)
	}
}

// RequireCount fails the test unless exactly n events have been recorded.
func (r *Recorder) RequireCount(t testing.TB, n int) {
	t.Helper()
	if got := r.Count(); got != n {
		t.Fatalf("wlogtest: recorded %d events, want %d", got, n)
	}
}
