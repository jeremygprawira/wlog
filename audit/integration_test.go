package audit_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/sample"
)

// TestAudit_RefundScenario is the Checkpoint 2B integration test from SPEC.md criterion
// 11: a refund handler's audit.Do call must survive a 0% sampler, land in both the main
// drain and the journal, and audit.Verify must pass on the journal and fail the moment
// one byte in it changes.
func TestAudit_RefundScenario(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	mem := memory.New(0) // stands in for "the main drain"
	log := wlog.New(
		wlog.WithHeadSampler(sample.MustNew(sample.Rate(wlog.LevelInfo, 0))), // keep 0% of events
		wlog.WithDrains(audit.Journal(path), mem),
	)
	ctx := log.WithContext(context.Background())

	audit.Do(ctx, audit.Record{
		Actor:   audit.Actor{Type: "user", ID: "agent42", Email: "agent@example.com"},
		Action:  "invoice.refund",
		Target:  audit.Target{Type: "invoice", ID: "inv-999"},
		Outcome: "success",
		Reason:  "customer request",
	})

	if events := mem.Snapshot(); len(events) != 1 {
		t.Fatalf("main drain got %d events, want 1 (0%% sampler must not drop an audit event)", len(events))
	}

	if err := audit.Verify(path); err != nil {
		t.Fatalf("Verify on an untouched journal: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	tampered := bytes.Replace(b, []byte("success"), []byte("denied!"), 1)
	if bytes.Equal(tampered, b) {
		t.Fatal("test bug: tampering did not change the journal bytes")
	}
	tamperedPath := filepath.Join(t.TempDir(), "tampered.ndjson")
	if err := os.WriteFile(tamperedPath, tampered, 0o600); err != nil {
		t.Fatalf("write tampered journal: %v", err)
	}
	if err := audit.Verify(tamperedPath); err == nil {
		t.Fatal("Verify accepted a journal with one changed byte")
	}
}
