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

// writeJournal writes n records and closes the Logger, so the journal releases its
// file lock and writes its closing marker. A second writer on the same path needs that
// release, because one journal file has one writer at a time.
func writeJournal(t *testing.T, path string, n int) {
	t.Helper()
	log := wlog.New(wlog.WithDrains(audit.Journal(path)))
	ctx := log.WithContext(context.Background())
	for i := 0; i < n; i++ {
		r := testRecord()
		r.Target.ID = "inv" + strconv.Itoa(i)
		audit.Do(ctx, r)
	}
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
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

	records := 0
	for _, line := range journalLines(t, path) {
		if !strings.Contains(line, `"audit.marker"`) {
			records++
		}
	}
	if records != 3 {
		t.Fatalf("got %d record lines, want 3", records)
	}
}

func TestAudit_Journal_ResumesChainAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")

	writeJournal(t, path, 2) // first "process"
	writeJournal(t, path, 2) // "restart": a fresh Journal(path) instance

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify after resume: %v", err)
	}
	// Four records in one file, plus each run's closing marker.
	if lines := journalLines(t, path); len(lines) != 6 {
		t.Fatalf("got %d lines, want 4 records and 2 markers", len(lines))
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
	records := 0
	for _, line := range journalLines(t, path) {
		if !strings.Contains(line, `"audit.marker"`) {
			records++
		}
	}
	if records != n {
		t.Fatalf("got %d record lines, want %d", records, n)
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

	firstRun := journalLines(t, path)

	writeJournal(t, path, 1) // a new process appends one more line

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify after restart: %v", err)
	}

	lines := journalLines(t, path)
	if len(lines) != len(firstRun)+2 {
		t.Fatalf("got %d lines, want %d (one record and one marker more)", len(lines), len(firstRun)+2)
	}
	var first, last map[string]any
	if err := json.Unmarshal([]byte(firstRun[len(firstRun)-1]), &last); err != nil {
		t.Fatalf("decode last line of the first run: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[len(firstRun)]), &first); err != nil {
		t.Fatalf("decode first line after the restart: %v", err)
	}
	if got, want := first["audit.prev_hash"], last["audit.hash"]; got != want {
		t.Errorf("the restarted journal started from %v, want the previous last hash %v", got, want)
	}
}

// writeJournalClosed writes n audit records and closes the Logger, so Journal writes
// its final marker line.
func writeJournalClosed(t *testing.T, path string, n int, opts ...audit.Option) {
	t.Helper()
	log := wlog.New(wlog.WithDrains(audit.Journal(path, opts...)))
	ctx := log.WithContext(context.Background())
	for i := 0; i < n; i++ {
		r := testRecord()
		r.Target.ID = "inv" + strconv.Itoa(i)
		audit.Do(ctx, r)
	}
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// journalLines returns the non-empty lines of a journal file.
func journalLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// TestAudit_AUD1_EmptyFileFails proves that Verify rejects an empty journal instead of
// reporting success for a file with nothing in it.
func TestAudit_AUD1_EmptyFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.ndjson")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := audit.Verify(path); err == nil {
		t.Error("Verify accepted an empty journal")
	}
}

// TestAudit_AUD1_TruncationFails proves that marker lines catch a cut journal. Cutting
// lines from the end removes the marker, which only an expected head can notice.
func TestAudit_AUD1_TruncationFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournalClosed(t, path, 3)

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify the full journal: %v", err)
	}
	lines := journalLines(t, path)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 3 records and a marker", len(lines))
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &last); err != nil {
		t.Fatalf("decode the marker: %v", err)
	}
	head, _ := last["audit.hash"].(string)

	// Cutting the marker line leaves a valid-looking chain, so only the expected head
	// from outside the file can catch it.
	cut := filepath.Join(t.TempDir(), "cut.ndjson")
	if err := os.WriteFile(cut, []byte(strings.Join(lines[:3], "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := audit.VerifyHead(cut, head)
	if err == nil {
		t.Fatal("VerifyHead accepted a journal cut after its last record")
	}
	if !strings.Contains(err.Error(), "head") {
		t.Errorf("error = %v, want a head mismatch", err)
	}
	if err := audit.VerifyHead(path, head); err != nil {
		t.Errorf("VerifyHead on the full journal: %v", err)
	}

	// Cutting a record in the middle leaves the marker's count and head behind.
	mid := filepath.Join(t.TempDir(), "mid.ndjson")
	if err := os.WriteFile(mid, []byte(strings.Join(append(lines[:1], lines[2:]...), "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := audit.Verify(mid); err == nil {
		t.Error("Verify accepted a journal with a record cut from the middle")
	}
}

// TestAudit_AUD10_SignedNeedsKey proves that a signed journal verifies only under its
// key, that every marker carries a key id, and that VerifySigned rejects an unsigned
// journal even when the key is right.
func TestAudit_AUD10_SignedNeedsKey(t *testing.T) {
	key := []byte("key-one")
	path := filepath.Join(t.TempDir(), "signed.ndjson")
	writeJournalClosed(t, path, 3, audit.WithKey(key))

	if err := audit.Verify(path, key); err != nil {
		t.Fatalf("Verify with the right key: %v", err)
	}
	if err := audit.Verify(path, []byte("key-two")); err == nil {
		t.Error("Verify accepted the wrong key")
	}
	if err := audit.VerifySigned(path, key); err != nil {
		t.Fatalf("VerifySigned with the right key: %v", err)
	}

	lines := journalLines(t, path)
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("decode the closing marker: %v", err)
	}
	m, ok := last["audit.marker"].(map[string]any)
	if !ok {
		t.Fatalf("the last line is not a marker: %s", lines[len(lines)-1])
	}
	if id, _ := m["key_id"].(string); id == "" {
		t.Error("the signed marker carries no key_id")
	}

	plain := filepath.Join(t.TempDir(), "plain.ndjson")
	writeJournalClosed(t, plain, 2)
	if err := audit.VerifySigned(plain, key); err == nil {
		t.Error("VerifySigned accepted an unsigned journal")
	}
	if err := audit.Verify(plain); err != nil {
		t.Errorf("Verify on an unsigned journal: %v", err)
	}
}

// TestAudit_AUD10_MarkerEveryHundredRecords proves a marker lands every 100 records and
// once more on Close.
func TestAudit_AUD10_MarkerEveryHundredRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournalClosed(t, path, 100)

	lines := journalLines(t, path)
	if len(lines) != 102 {
		t.Fatalf("got %d lines, want 100 records, a marker at 100, and a closing marker", len(lines))
	}
	for _, i := range []int{100, 101} {
		var rec map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &rec); err != nil {
			t.Fatalf("decode line %d: %v", i+1, err)
		}
		m, ok := rec["audit.marker"].(map[string]any)
		if !ok {
			t.Fatalf("line %d holds no marker: %s", i+1, lines[i])
		}
		if got, _ := m["count"].(float64); int(got) != 100 {
			t.Errorf("marker at line %d has count %v, want 100", i+1, m["count"])
		}
	}
	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
