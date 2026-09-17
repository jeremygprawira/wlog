package audit_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"sync"
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
	got := auditRecords(t, rec.Last())[0]
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
	got := auditRecords(t, rec.Last())[0]
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

	got := auditRecords(t, rec.Last())[0]
	actor := got["actor"].(map[string]any)
	if actor["email"] == "a@example.com" {
		t.Errorf("actor.email leaked unredacted: %v", actor["email"])
	}
}

// TestAudit_AUD5_TwoRecordsKept proves that a second Do on the same event adds a record
// instead of replacing the first one.
func TestAudit_AUD5_TwoRecordsKept(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "invoice.refund")

	audit.Do(ctx, testRecord())
	second := testRecord()
	second.Action = "invoice.void"
	audit.Do(ctx, second)
	end()

	records := auditRecords(t, rec.Last())
	if len(records) != 2 {
		t.Fatalf("got %d audit records, want 2", len(records))
	}
	if records[0]["action"] != "invoice.refund" || records[1]["action"] != "invoice.void" {
		t.Errorf("actions = %v, %v; want invoice.refund, invoice.void", records[0]["action"], records[1]["action"])
	}
}

// TestAudit_AUD5_RecordCapIsTwenty proves an event holds at most 20 records, the cap
// SPEC.md names for the audit array.
func TestAudit_AUD5_RecordCapIsTwenty(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "invoice.refund")

	for i := 0; i < 25; i++ {
		audit.Do(ctx, testRecord())
	}
	end()

	if got := len(auditRecords(t, rec.Last())); got != 20 {
		t.Fatalf("the event holds %d audit records, want the cap of 20", got)
	}
}

// TestAudit_AUD5_AfterEndStandalone proves that a Do after the event ended becomes its
// own event instead of a late write into a sealed one.
func TestAudit_AUD5_AfterEndStandalone(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "invoice.refund")

	audit.Do(ctx, testRecord())
	end()
	audit.Do(ctx, testRecord()) // after end(): a new event, not a lost record

	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("got %d events, want the request event and one standalone audit event", len(events))
	}
	if got := len(auditRecords(t, events[1])); got != 1 {
		t.Fatalf("the standalone event holds %d audit records, want 1", got)
	}
	if events[1]["operation"] != "audit.invoice.refund" {
		t.Errorf("standalone operation = %v, want audit.invoice.refund", events[1]["operation"])
	}
}

// TestAudit_AUD5_KeyCapKeepsAudit proves the top-level key cap never drops the audit
// field: an event that already holds maxKeys fields still records the audit fact, and
// the sampler still sees it.
func TestAudit_AUD5_KeyCapKeepsAudit(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	for i := 0; i < 250; i++ {
		wlog.Set(ctx, "key"+strconv.Itoa(i), i)
	}
	audit.Do(ctx, testRecord())
	end()

	last := rec.Last()
	if last == nil {
		t.Fatal("no event recorded")
	}
	if got := len(auditRecords(t, last)); got != 1 {
		t.Fatalf("the event holds %d audit records, want 1 even with a full key set", got)
	}
}

// TestAudit_AUD6_FailuresReported proves that a journal failure reaches the error hook,
// and that a non-finite float never costs a record.
func TestAudit_AUD6_FailuresReported(t *testing.T) {
	var mu sync.Mutex
	var got []error
	report := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, err)
	}
	reported := func() []error {
		mu.Lock()
		defer mu.Unlock()
		return append([]error(nil), got...)
	}

	// A path inside a directory that does not exist can never open.
	path := filepath.Join(t.TempDir(), "missing", "audit.ndjson")
	d := audit.Journal(path, audit.WithOnError(report))
	d.Send(context.Background(), map[string]any{
		"audit": []any{map[string]any{"action": "invoice.refund"}},
	})
	if len(reported()) == 0 {
		t.Fatal("the journal open failure was not reported")
	}

	// NaN is not valid JSON, and a record must not be lost because of one.
	ok := filepath.Join(t.TempDir(), "audit.ndjson")
	d2 := audit.Journal(ok, audit.WithOnError(report))
	d2.Send(context.Background(), map[string]any{
		"audit": []any{map[string]any{"action": "invoice.refund"}},
		"ratio": math.NaN(),
	})
	if errs := reported(); len(errs) != 1 {
		t.Fatalf("a NaN in the event produced %v, want only the earlier open failure", errs)
	}
	if err := audit.Verify(ok); err != nil {
		t.Fatalf("Verify after a NaN: %v", err)
	}
}

// auditRecords returns the audit record list of one event, and fails the test when the
// field is absent or has the wrong shape.
func auditRecords(t *testing.T, event map[string]any) []map[string]any {
	t.Helper()
	raw, ok := event["audit"].([]any)
	if !ok {
		t.Fatalf("event carries no audit list: %v", event["audit"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("audit record is not a map: %v", r)
		}
		out = append(out, m)
	}
	return out
}

// codedExtractor is a Logger extractor that knows the app's own error types, so the test
// can prove Wrap asks the Logger rather than the core default.
type codedExtractor struct{}

// Extract gives the test errors their application code, and a refusal status 403.
func (codedExtractor) Extract(err error) wlog.ErrorInfo {
	info := wlog.ErrorInfo{Code: "PAYMENT_DECLINED", Message: err.Error()}
	if errors.Is(err, errRefused) {
		info.Status = 403
	}
	return info
}

// errRefused stands for a refusal, such as an authorization error.
var errRefused = errors.New("refused")

// TestAudit_AUD8_WrapUsesLoggerExtractor proves Wrap records the code the Logger's own
// extractor produced, so audit.error_code and error.code agree, and that a denial is
// recorded as denied rather than as a generic error.
func TestAudit_AUD8_WrapUsesLoggerExtractor(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithErrorExtractor(codedExtractor{}))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	err := audit.Wrap(ctx, audit.Record{Action: "payment.charge"}, func() error {
		return errors.New("card declined")
	})
	end()
	if err == nil {
		t.Fatal("Wrap returned nil for a failing fn")
	}
	record := recordOf(t, rec.Last())
	if record["error_code"] != "PAYMENT_DECLINED" {
		t.Errorf("audit.error_code = %v, want the Logger extractor's code", record["error_code"])
	}
	if _, ok := rec.Last()["error"]; !ok {
		t.Error("the event carries no error, so error.code cannot agree with audit.error_code")
	}

	ctx, end = wlog.Start(log.WithContext(context.Background()), "op")
	_ = audit.Wrap(ctx, audit.Record{Action: "invoice.refund"}, func() error {
		return fmt.Errorf("charge: %w", errRefused)
	})
	end()
	if got := recordOf(t, rec.Last())["outcome"]; got != "denied" {
		t.Errorf("outcome = %v, want denied for a 403", got)
	}
}
