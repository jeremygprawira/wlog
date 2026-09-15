package audit_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	os.WriteFile(tampered, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

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
	os.WriteFile(tampered, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

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
	os.WriteFile(tampered, []byte(strings.Join(lines, "\n")+"\n"), 0o600)

	err := audit.Verify(tampered)
	if err == nil {
		t.Fatal("Verify accepted reordered lines")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error = %v, want it to name line 1", err)
	}
}
