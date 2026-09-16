package file_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/file"
	"github.com/jeremygprawira/wlog/drain/memory"
)

// write appends one line to path.
func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestFile_Read proves Read returns every good line, skips a malformed one, and counts
// the skips.
func TestFile_Read(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	appendLine(t, path, `{"level":"info","operation":"a"}`)
	appendLine(t, path, `{"level":"error","operation":"b"}`)
	appendLine(t, path, `this is not json`)
	appendLine(t, path, `{"level":"error","operation":"c"}`)

	events, skipped, err := file.Read(path, memory.Filter{Level: "error"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Read returned %d events, want 2", len(events))
	}
	if events[0]["operation"] != "b" || events[1]["operation"] != "c" {
		t.Errorf("events = %v, want b then c", events)
	}
	if len(skipped.Lines) != 1 || skipped.Lines[0] != 3 {
		t.Errorf("ParseErrors = %+v, want line 3", skipped)
	}
	if skipped.Error() == "" {
		t.Error("ParseErrors.Error() is empty with one skipped line")
	}
}

// TestFile_Tail proves Tail delivers a line appended after it started, survives a
// rotation, and closes its channel when the context is done.
func TestFile_Tail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	appendLine(t, path, `{"level":"info","operation":"first"}`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := file.Tail(ctx, path, memory.Filter{})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}

	if event := next(t, out); event["operation"] != "first" {
		t.Fatalf("first tail event = %v, want first", event)
	}

	appendLine(t, path, `{"level":"info","operation":"second"}`)
	if event := next(t, out); event["operation"] != "second" {
		t.Fatalf("appended tail event = %v, want second", event)
	}

	// Rotate: move the old file aside and start a new one at the same path.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	appendLine(t, path, `{"level":"info","operation":"rotated"}`)
	if event := next(t, out); event["operation"] != "rotated" {
		t.Fatalf("rotated tail event = %v, want rotated", event)
	}

	cancel()
	select {
	case _, open := <-out:
		if open {
			t.Error("channel still open after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Error("channel did not close after cancel")
	}
}

// next waits for one event, failing the test after two seconds.
func next(t *testing.T, out <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case event := <-out:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no event within two seconds")
		return nil
	}
}
