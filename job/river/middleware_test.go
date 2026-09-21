// This file runs the work conformance suite against the worker path, and checks the fields,
// the result, and the panic rule of the worker middleware.
package wlogriver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestRiver_C1_WorkConformance proves that the worker path passes every scenario of the work
// suite.
func TestRiver_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path the worker middleware uses. The suite
// supplies the unit, because one River job carries no rpc, message, command, or function
// field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestRiver_C1_EventNamesTheJob proves that one worked job records the job group with the
// fields of the table, and the lag from its scheduled time.
func TestRiver_C1_EventNamesTheJob(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := &rivertype.JobRow{
		ID: 42, Kind: "reindex", Queue: "critical", Attempt: 2, MaxAttempts: 5,
		ScheduledAt: time.Now().Add(-2 * time.Second),
	}

	if err := New(log).Work(context.Background(), job, succeed); err != nil {
		t.Fatalf("Work returned %v", err)
	}
	got := lastEvent(t, rec)
	if got["operation"] != "job reindex" {
		t.Errorf("operation = %v, want job reindex", got["operation"])
	}
	fields, _ := got["job"].(map[string]any)
	for key, want := range map[string]any{
		"system": "river", "name": "reindex", "id": "42",
		"queue": "critical", "attempt": 2, "max_attempts": 5,
	} {
		if !conformance.Equal(fields[key], want) {
			t.Errorf("job.%s = %v, want %v", key, fields[key], want)
		}
	}
	if lag, _ := fields["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("job.lag_ms = %v, want about 2000", fields["lag_ms"])
	}
}

// TestRiver_C5_SnoozeRecordsSnoozeAndInfo proves that a job that snoozes records result
// snooze and level info, which wins over the error rule.
func TestRiver_C5_SnoozeRecordsSnoozeAndInfo(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := &rivertype.JobRow{Kind: "reindex", Attempt: 1, MaxAttempts: 3}

	err := New(log).Work(context.Background(), job, func(context.Context) error {
		return river.JobSnooze(time.Minute)
	})
	if !errors.Is(err, &river.JobSnoozeError{}) {
		t.Fatalf("Work returned %v, want the snooze error", err)
	}
	got := lastEvent(t, rec)
	if got["level"] != "info" || got["outcome"] != "success" {
		t.Errorf("level/outcome = %v/%v, want info/success", got["level"], got["outcome"])
	}
	if result := jobField(t, got, "result"); result != "snooze" {
		t.Errorf("job.result = %v, want snooze", result)
	}
}

// TestRiver_C1_CancelRecordsCancelAndWarn proves that a cancelled job records result cancel
// and level warn, which wins over the error rule.
func TestRiver_C1_CancelRecordsCancelAndWarn(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := &rivertype.JobRow{Kind: "reindex", Attempt: 1, MaxAttempts: 3}

	_ = New(log).Work(context.Background(), job, func(context.Context) error {
		return river.JobCancel(errString("operator stop"))
	})

	got := lastEvent(t, rec)
	if got["level"] != "warn" {
		t.Errorf("level = %v, want warn", got["level"])
	}
	if result := jobField(t, got, "result"); result != "cancel" {
		t.Errorf("job.result = %v, want cancel", result)
	}
}

// TestRiver_C1_LastAttemptRecordsDiscard proves that an error on the last attempt records
// result discard.
func TestRiver_C1_LastAttemptRecordsDiscard(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := &rivertype.JobRow{Kind: "reindex", Attempt: 3, MaxAttempts: 3}

	_ = New(log).Work(context.Background(), job, func(context.Context) error { return errString("boom") })

	got := lastEvent(t, rec)
	if got["level"] != "error" {
		t.Errorf("level = %v, want error", got["level"])
	}
	if result := jobField(t, got, "result"); result != "discard" {
		t.Errorf("job.result = %v, want discard", result)
	}
}

// TestRiver_C1_ErrorRecordsRetry proves that an error with attempts left records result
// retry.
func TestRiver_C1_ErrorRecordsRetry(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := &rivertype.JobRow{Kind: "reindex", Attempt: 1, MaxAttempts: 3}

	_ = New(log).Work(context.Background(), job, func(context.Context) error { return errString("boom") })

	if result := jobField(t, lastEvent(t, rec), "result"); result != "retry" {
		t.Errorf("job.result = %v, want retry", result)
	}
}

// TestRiver_C1_PanicRecordsStackAndPanics proves that a panicking job records one error
// event with a stack, and the middleware panics again, so River keeps its own panic
// behavior.
func TestRiver_C1_PanicRecordsStackAndPanics(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := &rivertype.JobRow{Kind: "reindex", Attempt: 1, MaxAttempts: 3}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("the middleware did not panic again")
			}
		}()
		_ = New(log).Work(context.Background(), job, func(context.Context) error { panic("boom") })
	}()

	info, _ := lastEvent(t, rec)["error"].(map[string]any)
	if info == nil || info["stack"] == nil {
		t.Errorf("error = %v, want the recovered stack", info)
	}
}

// TestRiver_C1_MetadataLinksTrace proves that the worker reads the trace of the insert
// metadata, so a job joins the trace of its inserter.
func TestRiver_C1_MetadataLinksTrace(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := &rivertype.JobRow{
		Kind: "reindex", Attempt: 1, MaxAttempts: 3,
		Metadata: []byte(`{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}`),
	}

	_ = New(log).Work(context.Background(), job, succeed)

	trace, _ := lastEvent(t, rec)["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the metadata", trace["trace_id"])
	}
	if trace["parent_span_id"] != "00f067aa0ba902b7" {
		t.Errorf("trace.parent_span_id = %v, want the span id of the metadata", trace["parent_span_id"])
	}
}

// succeed is the inner job of a job that finishes without an error.
func succeed(context.Context) error { return nil }

// lastEvent returns the only recorded event, and stops the test when the run recorded none.
func lastEvent(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	return got
}

// jobField returns one field of the job group of an event.
func jobField(t *testing.T, got map[string]any, key string) any {
	t.Helper()
	job, _ := got["job"].(map[string]any)
	if job == nil {
		t.Fatalf("the job group is missing from %v", got)
	}
	return job[key]
}

// errString is the plain error a test returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
