package file_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/drain/file"
	"github.com/jeremygprawira/wlog/drain/memory"
)

// TestFile_PIPE12_LongLineSkipped proves a line over 1 MiB is skipped and counted, and
// that the lines after it still arrive: one bad line never fails a read.
func TestFile_PIPE12_LongLineSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	long := `{"operation":"` + strings.Repeat("x", 2<<20) + `"}`
	content := strings.Join([]string{
		`{"operation":"first"}`,
		long,
		`{"operation":"last"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	events, skipped, err := file.Read(path, memory.Filter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Read returned %d events, want the lines around the oversized one", len(events))
	}
	if events[0]["operation"] != "first" || events[1]["operation"] != "last" {
		t.Errorf("operations = %v, %v; want first, last", events[0]["operation"], events[1]["operation"])
	}
	if len(skipped.Lines) != 1 || skipped.Lines[0] != 2 {
		t.Fatalf("skipped = %v, want the one line at line 2", skipped.Lines)
	}
	if skipped.Error() == "" {
		t.Error("the oversized line was not reported")
	}
}

// TestFile_PIPE12_TailRotationSameSize proves Tail notices a rotation by file identity,
// not by size, and drains the old file first. The new file already holds as many bytes as
// the old offset, which is the case the size check missed.
func TestFile_PIPE12_TailRotationSameSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.ndjson")
	oldLine := `{"operation":"old-1"}`
	if err := os.WriteFile(path, []byte(oldLine+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines, err := file.Tail(ctx, path, memory.Filter{})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got := firstOperation(t, lines); got != "old-1" {
		t.Fatalf("first line = %q, want old-1", got)
	}

	// Rotate: the old file keeps its name plus .1, and the new file at the same path
	// already holds a line of the same length, so its size equals the old offset.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	newLine := `{"operation":"new-1"}`
	if len(newLine) != len(oldLine) {
		t.Fatalf("the test needs equal-length lines, got %d and %d", len(newLine), len(oldLine))
	}
	if err := os.WriteFile(path, []byte(newLine+"\n"), 0o600); err != nil {
		t.Fatalf("write the new file: %v", err)
	}

	if got := firstOperation(t, lines); got != "new-1" {
		t.Fatalf("after rotation = %q, want the new file's line", got)
	}
}

// firstOperation reads one operation value from a Tail channel.
func firstOperation(t *testing.T, lines <-chan map[string]any) string {
	t.Helper()
	select {
	case event, ok := <-lines:
		if !ok {
			t.Fatal("the tail channel closed")
		}
		operation, _ := event["operation"].(string)
		return operation
	case <-time.After(3 * time.Second):
		t.Fatal("the tail channel produced nothing")
		return ""
	}
}

// TestFile_PAR21_DefaultFolder proves a drain with no path writes the daily file under
// .wlog/logs, keeps that folder out of the repository, and prunes old days to MaxFiles.
func TestFile_PAR21_DefaultFolder(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	d, err := file.NewSender(file.WithMaxFiles(3))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	defer func() { _ = d.Close(context.Background()) }()
	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "default"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, ".wlog", "logs", today+".jsonl")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the daily file was not written: %v", err)
	}
	if !strings.Contains(string(got), "default") {
		t.Errorf("daily file = %q, want the event", got)
	}
	gitignore, err := os.ReadFile(filepath.Join(dir, ".wlog", ".gitignore"))
	if err != nil {
		t.Fatalf("no .wlog/.gitignore: %v", err)
	}
	if strings.TrimSpace(string(gitignore)) != "*" {
		t.Errorf(".gitignore = %q, want *", gitignore)
	}

	// MaxFiles keeps the newest three days, so the older ones go.
	logs := filepath.Join(dir, ".wlog", "logs")
	for _, name := range []string{"2000-01-01.jsonl", "2000-01-02.jsonl", "2000-01-03.jsonl", "2000-01-04.jsonl"} {
		if err := os.WriteFile(filepath.Join(logs, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	second, err := file.NewSender(file.WithMaxFiles(3))
	if err != nil {
		t.Fatalf("second NewSender: %v", err)
	}
	defer func() { _ = second.Close(context.Background()) }()

	left, err := os.ReadDir(logs)
	if err != nil {
		t.Fatalf("read the logs folder: %v", err)
	}
	var names []string
	for _, entry := range left {
		names = append(names, entry.Name())
	}
	want := []string{"2000-01-03.jsonl", "2000-01-04.jsonl", today + ".jsonl"}
	if len(names) != len(want) {
		t.Fatalf("the folder holds %v, want the newest three %v", names, want)
	}
	for i, name := range names {
		if name != want[i] {
			t.Fatalf("the folder holds %v, want %v", names, want)
		}
	}
}

// TestFile_PIPE12_WriteAfterCloseFails proves a batch after Close is reported instead of
// reopening a file the caller closed.
func TestFile_PIPE12_WriteAfterCloseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	d, err := file.NewSender(file.WithPath(path))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "late"}}); err == nil {
		t.Fatal("SendBatch after Close returned nil")
	}
}

// chdir moves the test into dir and restores the working directory afterwards. It uses
// os.Chdir rather than t.Chdir, which arrived in Go 1.24, because the root module keeps a
// Go 1.21 floor.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
