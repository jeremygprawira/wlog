package audit_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/sample"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func testRecord() audit.Record {
	return audit.Record{
		Actor:   audit.Actor{Type: "user", ID: "u1", Email: "a@example.com"},
		Action:  "invoice.refund",
		Target:  audit.Target{Type: "invoice", ID: "inv1"},
		Outcome: "success",
		Reason:  "customer request",
	}
}

func TestAudit_Record_InsideStart_SetsFieldOnCurrentEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "http.request")

	audit.Do(ctx, testRecord())
	end()

	rec.RequireCount(t, 1)
	got, ok := rec.Last()["audit"].(map[string]any)
	if !ok {
		t.Fatalf("audit field missing or wrong type: %v", rec.Last())
	}
	if got["action"] != "invoice.refund" {
		t.Errorf("audit.action = %v, want invoice.refund", got["action"])
	}
	if rec.Last()["operation"] != "http.request" {
		t.Errorf("operation = %v, want http.request (no extra event created)", rec.Last()["operation"])
	}
}

func TestAudit_Record_OutsideStart_EmitsStandaloneEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	audit.Do(ctx, testRecord())

	rec.RequireCount(t, 1)
	if rec.Last()["operation"] != "audit.invoice.refund" {
		t.Errorf("operation = %v, want audit.invoice.refund", rec.Last()["operation"])
	}
	got, ok := rec.Last()["audit"].(map[string]any)
	if !ok {
		t.Fatalf("audit field missing or wrong type: %v", rec.Last())
	}
	if got["outcome"] != "success" {
		t.Errorf("audit.outcome = %v, want success", got["outcome"])
	}
}

func TestAudit_BypassesSampling(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithSampler(sample.New(sample.Rate(wlog.LevelInfo, 0))))
	ctx := log.WithContext(context.Background())

	audit.Do(ctx, testRecord())

	rec.RequireCount(t, 1)
}

func TestAudit_ActorEmailIsRedacted(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())

	audit.Do(ctx, testRecord())

	got := rec.Last()["audit"].(map[string]any)
	actor := got["actor"].(map[string]any)
	if actor["email"] == "a@example.com" {
		t.Errorf("actor.email leaked unredacted: %v", actor["email"])
	}
}
