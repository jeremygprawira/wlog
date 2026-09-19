// This file checks the follow path: appended lines arrive, and a rotated file is read
// from its own start.
package query

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	wlogquery "github.com/jeremygprawira/wlog/query"
)

// firstLine and secondLine are the two events the tail tests write.
const firstLine = `{"level":"error","operation":"POST /orders","summary":"first"}`
const secondLine = `{"level":"info","operation":"GET /orders","summary":"second"}`

// TestTail_FollowsAppends proves that a line appended after the first read reaches the
// printer.
func TestTail_FollowsAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	writeFile(t, path, firstLine+"\n")

	got := make(chan map[string]any, 8)
	done, cancel := startFollow(t, path, got)

	if event := nextEvent(t, got); event["summary"] != "first" {
		t.Errorf("first event = %v, want the line already in the file", event)
	}
	appendFile(t, path, secondLine+"\n")
	if event := nextEvent(t, got); event["summary"] != "second" {
		t.Errorf("second event = %v, want the appended line", event)
	}
	stopFollow(t, done, cancel)
}

// TestTail_FollowsRotation proves that a file replaced by a new file is read from the
// new file's own start.
func TestTail_FollowsRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.ndjson")
	writeFile(t, path, firstLine+"\n")

	got := make(chan map[string]any, 8)
	done, cancel := startFollow(t, path, got)
	if event := nextEvent(t, got); event["summary"] != "first" {
		t.Errorf("first event = %v, want the line already in the file", event)
	}

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	writeFile(t, path, secondLine+"\n")
	if event := nextEvent(t, got); event["summary"] != "second" {
		t.Errorf("second event = %v, want the line of the new file", event)
	}
	stopFollow(t, done, cancel)
}

// startFollow follows one file in a goroutine, and it reports the matches on got.
func startFollow(t *testing.T, path string, got chan map[string]any) (chan error, context.CancelFunc) {
	t.Helper()
	filter, err := wlogquery.Compile(wlogquery.Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- followFile(ctx, path, filter, func(event map[string]any) error {
			got <- event
			return nil
		})
	}()
	t.Cleanup(cancel)
	return done, cancel
}

// nextEvent waits for one event, and it fails the test after a few seconds.
func nextEvent(t *testing.T, got chan map[string]any) map[string]any {
	t.Helper()
	select {
	case event := <-got:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived in time")
		return nil
	}
}

// stopFollow cancels the follow loop and waits for it to return.
func stopFollow(t *testing.T, done chan error, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("follow returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the follow loop did not stop")
	}
}

// writeFile writes one file.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// appendFile appends one line to a file.
func appendFile(t *testing.T, path, body string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(body); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}
