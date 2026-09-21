// This file runs one job and compares the event with the recipe's hand-written golden. The
// schema tool validates the golden against schema/event.v1.json.
package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogcron "github.com/jeremygprawira/wlog/job/cron"
)

// TestCronJob_GoldenEvent proves that one run gives the event the recipe documents.
func TestCronJob_GoldenEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	wlogcron.Job(rec.Logger(), "reindex", spec, reindex).Run()

	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(golden(t)), got); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}
}

// golden reads the recipe's hand-written event.
func golden(t *testing.T) map[string]any {
	t.Helper()
	body, err := os.ReadFile("testdata/event.json")
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	event := map[string]any{}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("parse the golden: %v", err)
	}
	return event
}
