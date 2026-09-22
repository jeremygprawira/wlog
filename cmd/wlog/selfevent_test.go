// This file checks the self event of the tool: a run with WLOG_DRAINS set records one command
// event, and the output of the tool stays the same.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSelfEvent_RecordsTheRun proves that one run records one command event with the path and
// the exit code, and that the tool prints nothing extra.
func TestSelfEvent_RecordsTheRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("WLOG_DRAINS", "file")
	t.Setenv("WLOG_FILE_PATH", path)
	t.Setenv("WLOG_DEBUG", "")
	t.Setenv("WLOG_OUTPUT", "")

	var stdout, stderr bytes.Buffer
	code := runCommand([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	// The tool output is the output of a run without the drains.
	var plainOut, plainErr bytes.Buffer
	if plain := run([]string{"version"}, &plainOut, &plainErr); plain != 0 {
		t.Fatalf("the plain run returned %d", plain)
	}
	if stdout.String() != plainOut.String() {
		t.Errorf("stdout = %q, want the plain output %q", stdout.String(), plainOut.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing extra", stderr.String())
	}

	got := readSelfEvent(t, path)
	if got["kind"] != "command" || got["operation"] != "wlog version" {
		t.Errorf("kind/operation = %v/%v, want command/wlog version", got["kind"], got["operation"])
	}
	cli, _ := got["cli"].(map[string]any)
	if cli["path"] != "wlog version" {
		t.Errorf("cli.path = %v, want wlog version", cli["path"])
	}
	if code, ok := cli["exit_code"].(float64); !ok || code != 0 {
		t.Errorf("cli.exit_code = %v, want 0", cli["exit_code"])
	}
}

// TestSelfEvent_UnknownWordKeepsThePathClean proves that a word which names no command stays
// out of cli.path, and that a usage fault records level warn.
func TestSelfEvent_UnknownWordKeepsThePathClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("WLOG_DRAINS", "file")
	t.Setenv("WLOG_FILE_PATH", path)
	t.Setenv("WLOG_DEBUG", "")
	t.Setenv("WLOG_OUTPUT", "")

	var stdout, stderr bytes.Buffer
	if code := runCommand([]string{"nosuchcommand"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	got := readSelfEvent(t, path)
	cli, _ := got["cli"].(map[string]any)
	if cli["path"] != "wlog" {
		t.Errorf("cli.path = %v, want wlog", cli["path"])
	}
	if got["level"] != "warn" {
		t.Errorf("level = %v, want warn for a usage fault", got["level"])
	}
}

// TestSelfEvent_NoDrainsBuildsNoLogger proves that a run without WLOG_DRAINS writes no file.
func TestSelfEvent_NoDrainsBuildsNoLogger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("WLOG_DRAINS", "")
	t.Setenv("WLOG_FILE_PATH", path)

	var stdout, stderr bytes.Buffer
	if code := runCommand([]string{"version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the run wrote a file without WLOG_DRAINS: %v", err)
	}
}

// readSelfEvent returns the last event of one drain file.
func readSelfEvent(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the drain file: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	if len(lines) == 0 {
		t.Fatal("the drain file is empty")
	}
	var got map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &got); err != nil {
		t.Fatalf("parse the event: %v", err)
	}
	return got
}
