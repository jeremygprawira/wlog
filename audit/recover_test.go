package audit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
)

// closeJournal closes a Journal drain, which writes its final marker and releases the
// file lock.
func closeJournal(t *testing.T, d wlog.Drain) {
	t.Helper()
	c, ok := d.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("Journal has no Close")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Journal.Close: %v", err)
	}
}

// TestAudit_AUD9_SecondWriterRefused proves that one journal file has one writer: a
// second Journal on the same path writes nothing while the first holds it, and can take
// over after the first closes.
func TestAudit_AUD9_SecondWriterRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	event := map[string]any{"audit": []any{map[string]any{"action": "invoice.refund"}}}

	first := audit.Journal(path)
	first.Send(context.Background(), event)
	before := journalBytes(t, path)

	// Send is silent per gate G3, so the contract is observable: the file must not grow.
	second := audit.Journal(path)
	second.Send(context.Background(), event)
	if after := journalBytes(t, path); len(after) != len(before) {
		t.Fatalf("the second writer appended %d bytes while the first held the file", len(after)-len(before))
	}

	// Releasing the lock lets the next writer in.
	closeJournal(t, first)
	third := audit.Journal(path)
	third.Send(context.Background(), event)
	if after := journalBytes(t, path); len(after) <= len(before) {
		t.Fatal("the journal stayed locked after Close")
	}
	closeJournal(t, third)

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestAudit_AUD9_PartialLineRecovered proves that a half-written last line from a crash
// moves to <path>.partial, and the journal then resumes the chain.
func TestAudit_AUD9_PartialLineRecovered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	writeJournalClosed(t, path, 2) // the process that wrote these two records is gone

	// A crash in the middle of a write leaves bytes with no newline and no valid JSON.
	b := journalBytes(t, path)
	partial := `{"audit":{"action":"invoice.refund","outcome":"suc`
	if err := os.WriteFile(path, append(b, partial...), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	log := wlog.New(wlog.WithDrains(audit.Journal(path)))
	ctx := log.WithContext(context.Background())
	audit.Do(ctx, testRecord())
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify after recovering a partial line: %v", err)
	}
	kept, err := os.ReadFile(path + ".partial")
	if err != nil {
		t.Fatalf("the partial line was not kept: %v", err)
	}
	if !strings.Contains(string(kept), `"outcome":"suc`) {
		t.Errorf("the partial file holds %q, want the half line", kept)
	}
	for _, line := range journalLines(t, path) {
		if !strings.HasSuffix(line, "}") {
			t.Errorf("the journal still holds a half line: %q", line)
		}
	}
}

// TestAudit_AUD9_LongLine proves a 2 MB line is written, verified, and used to resume the
// chain, so a large record is never silently cut or skipped.
func TestAudit_AUD9_LongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	big := strings.Repeat("x", 2<<20)

	d := audit.Journal(path)
	d.Send(context.Background(), map[string]any{
		"audit": []any{map[string]any{"action": "invoice.refund"}},
		"blob":  big,
	})
	closeJournal(t, d)

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify a 2 MB line: %v", err)
	}

	// A restart must resume from that line rather than truncate it.
	log := wlog.New(wlog.WithDrains(audit.Journal(path)))
	audit.Do(log.WithContext(context.Background()), testRecord())
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify after a restart behind a 2 MB line: %v", err)
	}
}

// journalBytes reads a journal file.
func journalBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return b
}
