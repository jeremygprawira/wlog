package main

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAuditRefund_RecordsFact proves the refund rides on the request event as one audit
// record with actor, action, target, and outcome.
func TestAuditRefund_RecordsFact(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "http.request")
	refund(ctx, "ord-1", 12.50)
	end()

	last := rec.Last()
	if last["order_id"] != "ord-1" {
		t.Errorf("order_id = %v, want ord-1", last["order_id"])
	}
	record, _ := last["audit"].(map[string]any)
	if record == nil {
		t.Fatalf("no audit record: %v", last)
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
