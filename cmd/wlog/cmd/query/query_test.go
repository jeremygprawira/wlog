// This file checks wlog query: the filter flags, the four formats, the newest and oldest
// limits, the exit codes, and the skipped-line count.
package query_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wlogquery "github.com/jeremygprawira/wlog/cmd/wlog/cmd/query"
	"github.com/jeremygprawira/wlog/drain/memory"
)

// fixture returns the path of the event fixture.
func fixture(name string) string {
	return filepath.Join("..", "..", "testdata", "query", name)
}

// run runs one query and returns its exit code, stdout, and stderr.
func run(args ...string) (int, string, string) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := wlogquery.Run(args, stdout, stderr)
	return code, stdout.String(), stderr.String()
}

// TestQuery_FilterAndExit proves that a match exits 0, no match exits 1, and a read
// error exits 2.
func TestQuery_FilterAndExit(t *testing.T) {
	code, stdout, _ := run("--level", "error", "--format", "json", fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "PAYMENT_DECLINED") {
		t.Errorf("stdout = %q, want the declined order", stdout)
	}
	if code, _, _ := run("--text", "refunded", fixture("events.ndjson")); code != 1 {
		t.Errorf("exit = %d, want 1 for no match", code)
	}
	if code, _, stderr := run(fixture("missing.ndjson")); code != 2 || stderr == "" {
		t.Errorf("exit = %d with stderr %q, want 2", code, stderr)
	}
}

// TestQuery_Formats proves that each format prints the matches.
func TestQuery_Formats(t *testing.T) {
	const events = "events.ndjson"
	code, stdout, _ := run("--format", "summary", fixture(events))
	if code != 0 || !strings.Contains(stdout, "ERROR POST /orders/{id} 502") {
		t.Errorf("summary = %q, want the spec's line", stdout)
	}
	if !strings.Contains(stdout, "(fix: Ask the customer for another card.)") {
		t.Errorf("summary = %q, want the fix", stdout)
	}
	code, stdout, _ = run("--format", "json", fixture(events))
	if code != 0 {
		t.Fatalf("json exit = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 4 {
		t.Fatalf("json lines = %d, want 4", len(lines))
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("json line %q: %v", line, err)
		}
	}
	code, stdout, _ = run("--format", "table", "--fields", "level,operation", fixture(events))
	if code != 0 || !strings.Contains(stdout, "level") || !strings.Contains(stdout, "POST /orders/{id}") {
		t.Errorf("table = %q, want the header and a row", stdout)
	}
	code, stdout, _ = run("--format", "pretty", "--limit", "1", fixture(events))
	if code != 0 || !strings.Contains(stdout, "\n  ") {
		t.Errorf("pretty = %q, want indented JSON", stdout)
	}
}

// TestQuery_LimitNewestAndOldest proves that the newest N is the default and --oldest
// takes the first N.
func TestQuery_LimitNewestAndOldest(t *testing.T) {
	code, stdout, _ := run("--format", "json", "--limit", "2", fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.Contains(stdout, "POST /orders") || !strings.Contains(stdout, "orders consume") {
		t.Errorf("newest two = %q, want the last two events", stdout)
	}
	_, stdout, _ = run("--format", "json", "--limit", "1", "--oldest", fixture("events.ndjson"))
	if !strings.Contains(stdout, "POST /orders") {
		t.Errorf("oldest one = %q, want the first event", stdout)
	}
}

// TestQuery_SkippedLines proves that a line which is not a JSON object is counted and
// reported.
func TestQuery_SkippedLines(t *testing.T) {
	code, _, stderr := run(fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stderr, "skipped 1 lines") {
		t.Errorf("stderr = %q, want the skipped-line count", stderr)
	}
}

// TestQuery_WhereAndFields proves that a where condition filters and that --fields keeps
// only the named keys.
func TestQuery_WhereAndFields(t *testing.T) {
	code, stdout, _ := run("--where", "http.status>=500", "--format", "json", "--fields", "operation,http.status", fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &event); err != nil {
		t.Fatalf("json: %v", err)
	}
	if _, present := event["level"]; present {
		t.Errorf("event = %v, want only the named keys", event)
	}
	http, _ := event["http"].(map[string]any)
	if http["status"] != float64(502) {
		t.Errorf("http = %v, want the nested status", event["http"])
	}
}

// TestQuery_BadFlags proves that a usage error exits 2.
func TestQuery_BadFlags(t *testing.T) {
	if code, _, _ := run("--format", "xml", fixture("events.ndjson")); code != 2 {
		t.Errorf("exit = %d, want 2 for a bad format", code)
	}
	if code, _, _ := run("--where", "no-operator", fixture("events.ndjson")); code != 2 {
		t.Errorf("exit = %d, want 2 for a bad where", code)
	}
	if code, _, _ := run("--limit", "0", fixture("events.ndjson")); code != 2 {
		t.Errorf("exit = %d, want 2 for a zero limit", code)
	}
}

// TestQuery_Group proves the count per group, with hand-computed values.
func TestQuery_Group(t *testing.T) {
	code, stdout, _ := run("--group-by", "http.route", "--count", fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := "3  \n1  /orders/{id}\n"
	if stdout != want {
		t.Errorf("group counts = %q, want %q", stdout, want)
	}
}

// TestQuery_Stats proves the nearest-rank percentiles of the fixture, with hand-computed
// values.
func TestQuery_Stats(t *testing.T) {
	code, stdout, _ := run("--stats", "duration_ms", fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if want := "4  5.0  840.2  840.2  840.2\n"; stdout != want {
		t.Errorf("stats = %q, want %q", stdout, want)
	}

	code, stdout, _ = run("--group-by", "kind", "--stats", "duration_ms", fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := "request  2  12.5  840.2  840.2  840.2\n" +
		"job  1  5.0  5.0  5.0  5.0\n" +
		"message  1  3.0  3.0  3.0  3.0\n"
	if stdout != want {
		t.Errorf("group stats = %q, want %q", stdout, want)
	}
}

// TestQuery_Size proves that the size report holds one row per kind and operation.
func TestQuery_Size(t *testing.T) {
	code, stdout, _ := run("--size", fixture("events.ndjson"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 5 {
		t.Fatalf("size lines = %d, want a header and four rows:\n%s", len(lines), stdout)
	}
	if !strings.HasPrefix(lines[0], "kind") || !strings.Contains(lines[0], "gb_per_month") {
		t.Errorf("header = %q, want the size columns", lines[0])
	}
	if !strings.HasPrefix(lines[1], "request") || !strings.Contains(lines[1], "POST /orders/{id}") {
		t.Errorf("first row = %q, want the busiest request", lines[1])
	}
}

// TestQuery_URL proves that the same query against a live app's memory endpoint returns
// the same events as the query against the same events in a file.
func TestQuery_URL(t *testing.T) {
	events := []map[string]any{
		{"level": "info", "timestamp": "2026-09-16T08:00:00Z", "summary": "one"},
		{"level": "error", "timestamp": "2026-09-16T08:00:01Z", "summary": "two"},
		{"level": "error", "timestamp": "2026-09-16T08:00:02Z", "summary": "three"},
	}

	mem := memory.New(10)
	file := &bytes.Buffer{}
	for _, event := range events {
		mem.Send(context.Background(), event)
		body, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		file.Write(body)
		file.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "events.ndjson")
	if err := os.WriteFile(path, file.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	server := httptest.NewServer(mem.QueryHandler(memory.WithAnyAddress()))
	defer server.Close()

	code, fromURL, _ := run("--level", "error", "--oldest", "--format", "json", "--url", server.URL)
	if code != 0 {
		t.Fatalf("url exit = %d, want 0", code)
	}
	code, fromFile, _ := run("--level", "error", "--oldest", "--format", "json", path)
	if code != 0 {
		t.Fatalf("file exit = %d, want 0", code)
	}
	if fromURL != fromFile {
		t.Errorf("the URL query answered\n%s\nand the file query answered\n%s", fromURL, fromFile)
	}
	if strings.Contains(fromURL, "one") {
		t.Errorf("the info event reached the answer: %s", fromURL)
	}
}
