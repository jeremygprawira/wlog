package audit_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/catalog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAudit_Sign proves a signed journal verifies under its key and fails under another.
func TestAudit_Sign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.ndjson")
	key := []byte("fixed-key")
	log := wlog.New(wlog.WithDrains(audit.Journal(path, audit.WithKey(key))))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	audit.Do(ctx, audit.Record{Action: "invoice.refund", Outcome: "success"})
	end()
	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := audit.Verify(path, key); err != nil {
		t.Errorf("Verify with the right key: %v", err)
	}
	if err := audit.Verify(path, []byte("wrong-key")); err == nil {
		t.Error("Verify with the wrong key passed")
	}
	if err := audit.Verify(path); err != nil {
		t.Errorf("Verify without a key: %v", err)
	}
}

// TestAudit_Catalog_Reason proves a ReasonRequired entry marks a record with an empty
// reason instead of dropping it.
func TestAudit_Catalog_Reason(t *testing.T) {
	reg := catalog.New("invoice", catalog.Entry{
		Code:  "refund",
		Audit: &catalog.Audit{Action: "invoice.refund", TargetType: "invoice", ReasonRequired: true},
	})
	log, rec := wlogtest.New(t, wlog.WithEnrichers(audit.Catalog(reg)))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	audit.Do(ctx, audit.Record{Action: "invoice.refund", Target: audit.Target{ID: "inv-1"}})
	end()

	record, ok := rec.Last()["audit"].(map[string]any)
	if !ok {
		t.Fatalf("no audit record in %v", rec.Last())
	}
	if record["reason_missing"] != true {
		t.Errorf("reason_missing = %v, want true", record["reason_missing"])
	}
	target, _ := record["target"].(map[string]any)
	if target["type"] != "invoice" {
		t.Errorf("target.type = %v, want invoice", target["type"])
	}
	if target["id"] != "inv-1" {
		t.Errorf("target.id = %v, want inv-1 (the catalog must not overwrite it)", target["id"])
	}
}
