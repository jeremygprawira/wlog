package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAuditRefund_RecordsFact proves the refund rides on the request event as one audit
// record with actor, action, target, and outcome.
func TestAuditRefund_RecordsFact(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "refund.approve")
	refund(ctx, "ord-1", 12.50)
	end()

	last := rec.Last()
	if last["order_id"] != "ord-1" {
		t.Errorf("order_id = %v, want ord-1", last["order_id"])
	}
	records, _ := last["audit"].([]any)
	if len(records) == 0 {
		t.Fatalf("no audit record: %v", last)
	}
	record, _ := records[len(records)-1].(map[string]any)
	if record == nil {
		t.Fatalf("audit record is not a map: %v", records[len(records)-1])
	}
	if record["action"] != "refund.create" || record["outcome"] != "success" {
		t.Errorf("audit action/outcome = %v/%v, want refund.create/success", record["action"], record["outcome"])
	}
	target, _ := record["target"].(map[string]any)
	if target["id"] != "ord-1" {
		t.Errorf("audit target.id = %v, want ord-1", target["id"])
	}
	actor, _ := record["actor"].(map[string]any)
	if actor["id"] != "u-42" {
		t.Errorf("audit actor.id = %v, want u-42", actor["id"])
	}
}

// TestAuditRefund_JournalVerifies proves the example writes the refund to a signed
// journal, verifies it, and that a single changed byte makes Verify fail.
func TestAuditRefund_JournalVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.ndjson")
	if err := Run(path); err != nil {
		t.Fatalf("Run: %v", err)
	}

	journal, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if !strings.Contains(string(journal), "refund.create") {
		t.Errorf("the journal holds no refund record: %s", journal)
	}
	if !strings.Contains(string(journal), `"audit.signature"`) {
		t.Errorf("the journal is not signed: %s", journal)
	}

	tampered := filepath.Join(t.TempDir(), "tampered.ndjson")
	edited := bytes.Replace(journal, []byte("success"), []byte("denied!"), 1)
	if err := os.WriteFile(tampered, edited, 0o600); err != nil {
		t.Fatalf("write the tampered journal: %v", err)
	}
	if err := audit.Verify(tampered, journalKey); err == nil {
		t.Error("Verify accepted a tampered journal")
	}
}
