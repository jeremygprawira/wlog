// This file tests the output preset contract: a preset reshapes the JSON line a
// writer prints, and it never changes the canonical event a drain receives.
package wlog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/preset"
)

// TestPreset_CORE12_DrainsSeeCanonical proves that a preset changes the printed line
// only, so the drain still receives the canonical event with its nested groups.
func TestPreset_CORE12_DrainsSeeCanonical(t *testing.T) {
	mem := memory.New(0)
	log := wlog.New(
		wlog.WithService("shop", "1.2.3", "prod"),
		wlog.WithDrains(mem),
		wlog.WithOutput(preset.Flat()),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "GET /orders/{id}")
		wlog.SetGroup(ctx, "http", "method", "GET", "status", 200)
		wlog.Set(ctx, "order_id", "A-1")
		end()

		flushWriter(t, log)
	})

	last := mem.Snapshot()[0]
	if _, nested := last["http"].(map[string]any); !nested {
		t.Errorf("the drain saw http = %v, want the canonical nested object", last["http"])
	}
	if _, flat := last["http.status"]; flat {
		t.Errorf("the drain saw the flat key http.status: %v", last)
	}

	var printed map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("output is not one JSON line: %v\noutput: %q", err, out)
	}
	if printed["http.status"] != float64(200) {
		t.Errorf("printed http.status = %v, want 200", printed["http.status"])
	}
	if _, nested := printed["http"]; nested {
		t.Errorf("the printed line holds the canonical http object: %v", printed)
	}

	// The flat lead keys print first, in the order the preset names.
	keys := topKeys(t, out)
	want := []string{"timestamp", "level", "summary", "operation", "kind", "outcome", "duration_ms"}
	if len(keys) < len(want) || strings.Join(keys[:len(want)], ",") != strings.Join(want, ",") {
		t.Errorf("printed key order = %v, want the flat lead keys first", keys)
	}
}

// TestPreset_CORE12_PresetCannotChangeDrain proves that a preset receives its own copy
// of the event, so a preset that writes into the map never reaches a drain.
func TestPreset_CORE12_PresetCannotChangeDrain(t *testing.T) {
	mem := memory.New(0)
	log := wlog.New(
		wlog.WithDrains(mem),
		wlog.WithOutput(mutatingPreset{}),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.SetGroup(ctx, "http", "status", 200)
		wlog.Set(ctx, "order_id", "A-1")
		end()

		flushWriter(t, log)
	})

	last := mem.Snapshot()[0]
	http, _ := last["http"].(map[string]any)
	if http["status"] != int64(200) {
		t.Errorf("the drain saw http.status = %v, want 200", http["status"])
	}
	if last["order_id"] != "A-1" {
		t.Errorf("the drain saw order_id = %v, want A-1", last["order_id"])
	}
	if !strings.Contains(out, "999") {
		t.Errorf("the printed line does not hold what the preset wrote: %s", out)
	}
}

// TestPreset_PanicFallsBack proves that a preset which panics reports WLOG_HOOK_PANIC
// and writes the canonical event instead, so a broken preset never hides an event.
func TestPreset_PanicFallsBack(t *testing.T) {
	var problems []wlog.Problem
	log := wlog.New(
		wlog.WithOutput(panickingPreset{}),
		wlog.OnProblem(func(p wlog.Problem) { problems = append(problems, p) }),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.SetGroup(ctx, "http", "status", 200)
		wlog.Set(ctx, "order_id", "A-1")
		end()

		flushWriter(t, log)
	})

	if !strings.Contains(out, `"http":{"status":200}`) {
		t.Errorf("the canonical event was not printed: %s", out)
	}
	for _, p := range problems {
		if p.Code == "WLOG_HOOK_PANIC" && p.Source == "panicking" {
			return
		}
	}
	t.Errorf("problems = %+v, want WLOG_HOOK_PANIC from the panicking preset", problems)
}

// TestPreset_PrettyIgnoresPreset proves that the pretty console always renders the
// canonical event, because a preset names a backend dialect and not a developer view.
func TestPreset_PrettyIgnoresPreset(t *testing.T) {
	log := wlog.New(
		wlog.WithFormat(wlog.FormatPretty),
		wlog.WithOutput(mutatingPreset{}),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.SetGroup(ctx, "http", "status", 200)
		end()

		flushWriter(t, log)
	})

	if strings.Contains(out, "999") {
		t.Errorf("the pretty console used the preset: %s", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("the pretty console lost the canonical status: %s", out)
	}
}

// panickingPreset fails every time it shapes an event.
type panickingPreset struct{}

// Name identifies the preset in a report.
func (panickingPreset) Name() string { return "panicking" }

// Lead names the keys the writer prints first.
func (panickingPreset) Lead() []string { return []string{"level"} }

// Apply panics, so the writer must fall back to the canonical event.
func (panickingPreset) Apply(map[string]any) map[string]any { panic("preset boom") }

// mutatingPreset rewrites the event it is given, which a preset must never be able to
// do to the canonical event a drain received.
type mutatingPreset struct{}

// Name identifies the preset in a report.
func (mutatingPreset) Name() string { return "mutating" }

// Lead names the keys the writer prints first.
func (mutatingPreset) Lead() []string { return []string{"level"} }

// Apply changes the event in place and returns it.
func (mutatingPreset) Apply(event map[string]any) map[string]any {
	if http, ok := event["http"].(map[string]any); ok {
		http["status"] = 999
	}
	event["order_id"] = "changed"
	return event
}

// topKeys returns the top-level keys of a JSON line in the order they were written.
func topKeys(t *testing.T, line string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	if _, err := dec.Token(); err != nil {
		t.Fatalf("the line does not start an object: %v", err)
	}
	var keys []string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			t.Fatalf("reading a key: %v", err)
		}
		keys = append(keys, key.(string))
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatalf("reading the value of %q: %v", key, err)
		}
	}
	return keys
}
