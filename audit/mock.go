package audit

import (
	"context"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// Recorder collects the events a Mock logger emitted, so a test can assert on the audit
// fact without reading stdout.
type Recorder struct {
	mu     sync.Mutex
	events []map[string]any
}

// Send records one event. It implements wlog.Drain.
func (r *Recorder) Send(_ context.Context, event map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

// Events returns every recorded event, oldest first.
func (r *Recorder) Events() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, len(r.events))
	copy(out, r.events)
	return out
}

// Mock returns a logger that records instead of writing, plus its recorder. Use it in a
// test that exercises a handler calling audit.Do.
//
// The logger is silent, so a test prints no console line, and it is pinned to the debug
// level, so WLOG_LEVEL in the environment of a CI job can never hide a record from the
// test.
//
// Mock imports testing, so a production binary that imports audit links the testing
// package. Split this helper into its own package if that ever matters.
func Mock(t testing.TB) (*wlog.Logger, *Recorder) {
	t.Helper()
	recorder := &Recorder{}
	return wlog.New(
		wlog.WithFormat(wlog.FormatJSON),
		wlog.WithSilent(),
		wlog.WithLevel(wlog.LevelDebug),
		wlog.WithDrains(recorder),
	), recorder
}

// requireAudit returns the last audit record of the last event, and fails the test when
// there is none.
func (r *Recorder) requireAudit(t testing.TB) map[string]any {
	t.Helper()
	events := r.Events()
	if len(events) == 0 {
		t.Fatal("audit: no event recorded")
	}
	records, ok := events[len(events)-1][auditKey].([]any)
	if !ok || len(records) == 0 {
		t.Fatalf("audit: last event has no audit record: %v", events[len(events)-1])
	}
	record, ok := records[len(records)-1].(map[string]any)
	if !ok {
		t.Fatalf("audit: last audit record is not a map: %v", records[len(records)-1])
	}
	return record
}

// RequireAction fails the test unless the last audit record's action equals action.
func (r *Recorder) RequireAction(t testing.TB, action string) {
	t.Helper()
	if got := r.requireAudit(t)["action"]; got != action {
		t.Fatalf("audit action = %v, want %s", got, action)
	}
}

// RequireOutcome fails the test unless the last audit record's outcome equals outcome.
func (r *Recorder) RequireOutcome(t testing.TB, outcome string) {
	t.Helper()
	if got := r.requireAudit(t)["outcome"]; got != outcome {
		t.Fatalf("audit outcome = %v, want %s", got, outcome)
	}
}

// RequireActor fails the test unless the last audit record's actor matches.
func (r *Recorder) RequireActor(t testing.TB, actorType, id string) {
	t.Helper()
	actor, _ := r.requireAudit(t)["actor"].(map[string]any)
	if actor["type"] != actorType || actor["id"] != id {
		t.Fatalf("audit actor = %v, want type=%s id=%s", actor, actorType, id)
	}
}

// RequireNoAudit fails the test when the last event carries an audit record.
func (r *Recorder) RequireNoAudit(t testing.TB) {
	t.Helper()
	events := r.Events()
	if len(events) == 0 {
		t.Fatal("audit: no event recorded")
	}
	if _, ok := events[len(events)-1][auditKey]; ok {
		t.Fatalf("audit: last event carries an audit record: %v", events[len(events)-1])
	}
}
