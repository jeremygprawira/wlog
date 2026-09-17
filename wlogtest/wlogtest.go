// Package wlogtest lets a test assert on what its own code logged, without parsing
// JSON off stdout: New builds a real *wlog.Logger backed by an in-memory drain, and
// Recorder reads back what it recorded.
package wlogtest

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
)

// Recorder reads back the events a wlogtest-built Logger has emitted.
type Recorder struct {
	mem *memory.Memory
}

// New builds a *wlog.Logger with an in-memory drain, writing nothing at all, then applies any
// user opts on top, so a custom redactor, sampler, or anything else under test still takes
// effect. It is silent because a test that printed every event would drown its own report.
func New(t testing.TB, opts ...wlog.Option) (*wlog.Logger, *Recorder) {
	t.Helper()
	mem := memory.New(0)
	all := append([]wlog.Option{
		wlog.WithSilent(),
		wlog.WithDrains(mem),
	}, opts...)
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

// RequireField fails the test unless the last event's field equals want.
//
// The key may be a dotted path, so RequireField(t, "http.status", 500) reads the status
// inside the http group. Values compare with reflect.DeepEqual, so a slice, a map, or a
// number of another width still matches instead of panicking.
func (r *Recorder) RequireField(t testing.TB, key string, want any) {
	t.Helper()
	last := r.Last()
	if last == nil {
		t.Fatalf("wlogtest: no event recorded; wanted %s=%v", key, want)
	}
	got, ok := fieldAt(last, key)
	if !ok {
		t.Fatalf("wlogtest: no field %q in the last event: %v", key, last)
	}
	if !equalField(got, want) {
		t.Fatalf("wlogtest: %s = %#v, want %#v\nlast event: %v", key, got, want, last)
	}
}

// equalField compares one recorded value with the expected one.
//
// It uses reflect.DeepEqual, so a slice or a map compares by value instead of panicking, and a
// number matches across the widths an event may carry: an int literal in a test matches the
// int64 a Set stored, and the float64 a JSON round-trip produced.
func equalField(got, want any) bool {
	if reflect.DeepEqual(got, want) {
		return true
	}
	if gotNumber, ok := numberOf(got); ok {
		wantNumber, ok := numberOf(want)
		return ok && gotNumber == wantNumber
	}
	// A map or a slice holds values of widths the event chose, so the comparison walks in.
	switch g := got.(type) {
	case map[string]any:
		w, ok := want.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for key, value := range g {
			if !equalField(value, w[key]) {
				return false
			}
		}
		return true
	case []any:
		w, ok := want.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range g {
			if !equalField(g[i], w[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// numberOf reads any numeric width.
func numberOf(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

// fieldAt reads one field by a dotted path, such as "http.status".
func fieldAt(event map[string]any, path string) (any, bool) {
	var current any = event
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
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
