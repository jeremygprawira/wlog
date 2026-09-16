package file_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/file"
	"github.com/jeremygprawira/wlog/pipeline"
)

// readFile returns path's contents, or "" when it does not exist.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// lines returns the non-empty lines of s.
func lines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// TestFile_AppendNDJSON proves one batch appends one JSON line per event, with mode 0600.
func TestFile_AppendNDJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	d, err := file.New(file.WithPath(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	events := []map[string]any{{"level": "info", "operation": "a"}, {"level": "info", "operation": "b"}}
	if err := d.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}

	got := lines(readFile(t, path))
	if len(got) != 2 {
		t.Fatalf("file has %d lines, want 2:\n%s", len(got), readFile(t, path))
	}
	for _, line := range got {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("line is not JSON: %v (%q)", err, line)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}

// fillEvent returns an event large enough that two of them always pass a small maxSize.
func fillEvent(operation string) map[string]any {
	return map[string]any{"level": "info", "operation": operation, "padding": strings.Repeat("x", 200)}
}

// TestFile_RotateBySize proves a write that would pass maxSize rotates first, keeping
// the previous content in path.1.
func TestFile_RotateBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	d, err := file.New(file.WithPath(path), file.WithMaxSize(100))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	if err := d.SendBatch(context.Background(), []map[string]any{fillEvent("first")}); err != nil {
		t.Fatalf("SendBatch first: %v", err)
	}
	if err := d.SendBatch(context.Background(), []map[string]any{fillEvent("second")}); err != nil {
		t.Fatalf("SendBatch second: %v", err)
	}

	if got := readFile(t, path+".1"); !strings.Contains(got, "first") {
		t.Errorf("path.1 = %q, want the first batch", got)
	}
	if got := readFile(t, path); !strings.Contains(got, "second") {
		t.Errorf("path = %q, want the second batch", got)
	}
}

// TestFile_RotateByAge proves a file older than maxAge rotates before the next write.
func TestFile_RotateByAge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	d, err := file.New(file.WithPath(path), file.WithMaxAge(time.Nanosecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "first"}}); err != nil {
		t.Fatalf("SendBatch first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "second"}}); err != nil {
		t.Fatalf("SendBatch second: %v", err)
	}

	if got := readFile(t, path+".1"); !strings.Contains(got, "first") {
		t.Errorf("path.1 = %q, want the first batch", got)
	}
}

// TestFile_MaxBackups proves rotation keeps at most maxBackups files.
func TestFile_MaxBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	d, err := file.New(file.WithPath(path), file.WithMaxSize(10), file.WithMaxBackups(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	for i := 0; i < 3; i++ {
		if err := d.SendBatch(context.Background(), []map[string]any{fillEvent("batch")}); err != nil {
			t.Fatalf("SendBatch %d: %v", i, err)
		}
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Errorf("path.2 exists, want at most one backup")
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("path.1 missing: %v", err)
	}
}

// TestFile_ConcurrentBatches proves concurrent batches never interleave a line.
func TestFile_ConcurrentBatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	d, err := file.New(file.WithPath(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	const writers, perWriter = 8, 20
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				_ = d.SendBatch(context.Background(), []map[string]any{{"level": "info", "operation": "op"}})
			}
		}()
	}
	wg.Wait()

	got := lines(readFile(t, path))
	if len(got) != writers*perWriter {
		t.Fatalf("file has %d lines, want %d", len(got), writers*perWriter)
	}
	for _, line := range got {
		if err := json.Unmarshal([]byte(line), &map[string]any{}); err != nil {
			t.Errorf("interleaved line is not JSON: %v (%q)", err, line)
		}
	}
}

// TestFile_EnvAlone proves WLOG_FILE_PATH alone configures the drain.
func TestFile_EnvAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.ndjson")
	t.Setenv("WLOG_FILE_PATH", path)

	d, err := file.New()
	if err != nil {
		t.Fatalf("New from env: %v", err)
	}
	defer func() { _ = d.Close() }()
	if err := d.SendBatch(context.Background(), []map[string]any{{"operation": "env"}}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := readFile(t, path); !strings.Contains(got, "env") {
		t.Errorf("file = %q, want the env event", got)
	}
}

// TestFile_MissingPath proves a missing path is a construction error.
func TestFile_MissingPath(t *testing.T) {
	if _, err := file.New(); err == nil {
		t.Fatal("New with no path returned nil error")
	}
}

// TestFile_NeverLeaksRedactedValue proves gate G1 end to end.
func TestFile_NeverLeaksRedactedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g1.ndjson")
	d, err := file.New(file.WithPath(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	log := wlog.New(wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(1))))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "password", "hunter2")
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readFile(t, path)
	if got == "" {
		t.Fatal("file is empty, so the leak check proved nothing")
	}
	if strings.Contains(got, "hunter2") {
		t.Errorf("raw denied value reached the file: %s", got)
	}
}
