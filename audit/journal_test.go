package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
)

func writeJournal(t *testing.T, path string, n int) {
	t.Helper()
	log := wlog.New(wlog.WithDrains(audit.Journal(path)))
	ctx := log.WithContext(context.Background())
	for i := 0; i < n; i++ {
		r := testRecord()
		r.Target.ID = "inv" + strconv.Itoa(i)
		audit.Do(ctx, r)
	}
}

func TestAudit_Journal_WritesVerifiableNDJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournal(t, path, 3)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
}

func TestAudit_Journal_ResumesChainAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")

	writeJournal(t, path, 2) // first "process"
	writeJournal(t, path, 2) // "restart": a fresh Journal(path) instance

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify after resume: %v", err)
	}
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (2 before + 2 after restart)", len(lines))
	}
}

func TestAudit_Journal_FedThroughPipelineLikeAnyDrain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	var got map[string]any
	sideDrain := wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		got = event
	})
	log := wlog.New(wlog.WithDrains(audit.Journal(path), sideDrain))
	ctx := log.WithContext(context.Background())

	audit.Do(ctx, testRecord())

	if got == nil {
		t.Fatal("side drain never received the event")
	}
	if _, ok := got["audit.hash"]; !ok {
		t.Error("side drain's event has no audit.hash; Journal must run before it in the drain list")
	}
}

func TestAudit_Verify_DetectsEditedByte(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournal(t, path, 3)

	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	lines[1] = strings.Replace(lines[1], "success", "denied", 1)
	tampered := filepath.Join(t.TempDir(), "edited.ndjson")
	_ = os.WriteFile(tampered, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	err := audit.Verify(tampered)
	if err == nil {
		t.Fatal("Verify accepted an edited line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %v, want it to name line 2", err)
	}
}

func TestAudit_Verify_DetectsDeletedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournal(t, path, 3)

	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	lines = append(lines[:1], lines[2:]...) // delete line 2 (index 1)
	tampered := filepath.Join(t.TempDir(), "deleted.ndjson")
	_ = os.WriteFile(tampered, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	err := audit.Verify(tampered)
	if err == nil {
		t.Fatal("Verify accepted a journal with a deleted line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %v, want it to name line 2 (now the old line 3, whose prev_hash no longer matches)", err)
	}
}

func TestAudit_Verify_DetectsReorderedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournal(t, path, 3)

	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	lines[0], lines[1] = lines[1], lines[0]
	tampered := filepath.Join(t.TempDir(), "reordered.ndjson")
	_ = os.WriteFile(tampered, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	err := audit.Verify(tampered)
	if err == nil {
		t.Fatal("Verify accepted reordered lines")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error = %v, want it to name line 1", err)
	}
}

// TestAudit_AUD2_ConcurrentJournalVerifies proves that 100 concurrent audit records
// land in one chain and verify. Journal hashes and appends under a single lock, so the
// bytes on disk are always in chain order. Run with -count=20 to shake out the race.
func TestAudit_AUD2_ConcurrentJournalVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	log := wlog.New(wlog.WithDrains(audit.Journal(path)))
	ctx := log.WithContext(context.Background())

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := testRecord()
			r.Target.ID = "inv" + strconv.Itoa(i)
			audit.Do(ctx, r)
		}(i)
	}
	wg.Wait()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify after 100 concurrent records: %v", err)
	}
	b, _ := os.ReadFile(path)
	if lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n"); len(lines) != n {
		t.Fatalf("got %d lines, want %d", len(lines), n)
	}
}

// writeJournalWithNumbers writes one audit event holding a large integer and a small
// one, so the AUD-4 byte edits have a number to change.
func writeJournalWithNumbers(t *testing.T, path string) {
	t.Helper()
	log := wlog.New(wlog.WithDrains(audit.Journal(path)))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "invoice.refund")
	wlog.Set(ctx, "big", int64(9007199254740993))
	wlog.Set(ctx, "count", 1)
	audit.Do(ctx, testRecord())
	end()
}

// TestAudit_AUD4_ByteEditsFail proves that Verify hashes the exact bytes on disk, so
// every edit in AUD-4 fails, not only the ones that change parsed JSON.
func TestAudit_AUD4_ByteEditsFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournalWithNumbers(t, path)

	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := audit.Verify(path); err != nil {
		t.Fatalf("the untouched journal does not verify: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(string) string
	}{
		{"large integer off by one", func(s string) string {
			return strings.Replace(s, "9007199254740993", "9007199254740992", 1)
		}},
		{"integer written as a float", func(s string) string {
			return strings.Replace(s, `"count":1`, `"count":1.0`, 1)
		}},
		{"integer written with an exponent", func(s string) string {
			return strings.Replace(s, `"count":1`, `"count":1e0`, 1)
		}},
		{"added whitespace", func(s string) string {
			return strings.Replace(s, "{", "{ ", 1)
		}},
		{"CRLF line endings", func(s string) string {
			return strings.ReplaceAll(s, "\n", "\r\n")
		}},
		{"a blank line", func(s string) string { return s + "\n" }},
		{"a duplicate key", func(s string) string {
			return strings.Replace(s, "{", `{"audit.outcome":"forged",`, 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := filepath.Join(t.TempDir(), "tampered.ndjson")
			if err := os.WriteFile(tampered, []byte(tc.mutate(string(orig))), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			err := audit.Verify(tampered)
			if err == nil {
				t.Fatalf("Verify accepted %s", tc.name)
			}
			// Every edit names the line it broke. The appended newline is the second
			// line, because the first one still holds the record.
			want := "line 1"
			if tc.name == "a blank line" {
				want = "line 2"
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to name %s", err, want)
			}
		})
	}
}

// TestAudit_AUD3_RestartVerifies proves that a fresh Journal continues the chain the
// file already holds, rather than silently starting a new one.
func TestAudit_AUD3_RestartVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournal(t, path, 2)

	before, _ := os.ReadFile(path)
	firstRun := strings.Split(strings.TrimRight(string(before), "\n"), "\n")

	writeJournal(t, path, 1) // a new process appends one more line

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify after restart: %v", err)
	}

	after, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(after), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	var first, last map[string]any
	if err := json.Unmarshal([]byte(firstRun[1]), &last); err != nil {
		t.Fatalf("decode last line of the first run: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[2]), &first); err != nil {
		t.Fatalf("decode first line after the restart: %v", err)
	}
	if got, want := first["audit.prev_hash"], last["audit.hash"]; got != want {
		t.Errorf("the restarted journal started from %v, want the previous last hash %v", got, want)
	}
}
