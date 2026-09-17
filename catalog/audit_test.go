package catalog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/catalog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// refundPolicy is an audit policy that requires a reason and a change set, as a real
// refund does, and that redacts one path inside the change set.
func refundPolicy() *catalog.Registry {
	return catalog.New("invoice", catalog.Entry{
		Code: "refund",
		Audit: &catalog.Audit{
			Action:          "invoice.refund",
			TargetType:      "invoice",
			Severity:        "high",
			Description:     "An operator returned money to a customer.",
			ReasonRequired:  true,
			RequiresChanges: true,
			RedactPaths:     []string{"user.creds.nik"},
		},
	})
}

// TestCatalog_PAR11_ViolationsRecorded proves a record that breaks a policy rule is kept
// and marked, with every broken rule named, so a reader sees an incomplete fact instead of
// losing it.
func TestCatalog_PAR11_ViolationsRecorded(t *testing.T) {
	reg := refundPolicy()
	log, rec := wlogtest.New(t, wlog.WithEnrichers(audit.Catalog(reg)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "invoice.refund")
	audit.Do(ctx, audit.Record{
		Actor:  audit.Actor{Type: audit.ActorUser, ID: "u-1"},
		Action: "invoice.refund",
		Target: audit.Target{ID: "inv-1"},
	})
	end()

	record := auditRecord(t, rec.Last())
	violations, ok := record["violations"].([]any)
	if !ok || len(violations) != 2 {
		t.Fatalf("violations = %v, want reason_required and changes_required", record["violations"])
	}
	got := map[string]bool{}
	for _, v := range violations {
		got[v.(string)] = true
	}
	if !got["reason_required"] || !got["changes_required"] {
		t.Errorf("violations = %v, want reason_required and changes_required", violations)
	}
	if record["reason_missing"] != true {
		t.Errorf("reason_missing = %v, want true: the older field stays", record["reason_missing"])
	}

	// A complete record breaks no rule.
	ctx, end = wlog.Start(log.WithContext(context.Background()), "invoice.refund")
	audit.Do(ctx, audit.Record{
		Actor:   audit.Actor{Type: audit.ActorUser, ID: "u-1"},
		Action:  "invoice.refund",
		Target:  audit.Target{ID: "inv-1"},
		Reason:  "customer request",
		Changes: []audit.Operation{{Op: "replace", Path: "/status", Value: "refunded"}},
	})
	end()
	if record := auditRecord(t, rec.Last()); record["violations"] != nil {
		t.Errorf("a complete record carries violations: %v", record["violations"])
	}
	if auditRecord(t, rec.Last())["target"].(map[string]any)["type"] != "invoice" {
		t.Errorf("the policy's target type did not land: %v", auditRecord(t, rec.Last())["target"])
	}
}

// TestCatalog_PAR11_RedactPaths proves a path the policy redacts is masked inside the
// record's change set, so a patch describes the change without carrying the secret.
func TestCatalog_PAR11_RedactPaths(t *testing.T) {
	reg := refundPolicy()
	log, rec := wlogtest.New(t, wlog.WithEnrichers(audit.Catalog(reg)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "invoice.refund")
	audit.Do(ctx, audit.Record{
		Actor:  audit.Actor{Type: audit.ActorUser, ID: "u-1"},
		Action: "invoice.refund",
		Target: audit.Target{ID: "inv-1"},
		Reason: "customer request",
		Changes: []audit.Operation{
			{Op: "replace", Path: "/user/creds/nik", Value: "2222"},
			{Op: "replace", Path: "/status", Value: "refunded"},
		},
	})
	end()

	event := rec.Last()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "2222") {
		t.Errorf("the redacted path's value reached the event: %s", body)
	}
	record := auditRecord(t, event)
	changes, _ := record["changes"].([]any)
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want both operations", record["changes"])
	}
	nik, _ := changes[0].(map[string]any)
	if nik["path"] != "/user/creds/nik" {
		t.Errorf("the path was changed: %v", nik)
	}
	if text, _ := nik["value"].(string); text == "2222" {
		t.Errorf("the value was not masked: %v", nik)
	}
	status, _ := changes[1].(map[string]any)
	if status["value"] != "refunded" {
		t.Errorf("a path the policy allows was masked: %v", status)
	}
}

// auditRecord returns an event's last audit record, and fails the test when the event has
// none.
func auditRecord(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	records, ok := event["audit"].([]any)
	if !ok || len(records) == 0 {
		t.Fatalf("the event carries no audit record: %v", event)
	}
	record, ok := records[len(records)-1].(map[string]any)
	if !ok {
		t.Fatalf("the audit record is not a map: %v", records[len(records)-1])
	}
	return record
}
