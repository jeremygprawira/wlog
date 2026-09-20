// This file runs one command and compares the event with the recipe's hand-written
// golden. The schema tool validates the golden against schema/event.v1.json.
package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog/internal/conformance"
)

// TestCLITool_GoldenEvent proves that one run gives the event the recipe documents.
func TestCLITool_GoldenEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	code := run(context.Background(), rec.Logger(), []string{"--limit", "5"}, io.Discard)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}

	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	want := golden(t)
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(want), got); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}
}

// TestCLITool_UsageError proves that a bad flag exits 2 and records no event, because
// the flag set runs before the unit of work starts.
func TestCLITool_UsageError(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	code := run(context.Background(), rec.Logger(), []string{"--nope"}, io.Discard)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if len(rec.Events()) != 0 {
		t.Errorf("events = %d, want none before the unit of work starts", len(rec.Events()))
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
