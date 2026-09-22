// This file drives a real Temporal test environment, so one activity attempt proves the
// fields of the table end to end, and workflow code proves that it emits no event.
package wlogtemporal

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestTemporal_C6_WorkflowEmitsNoEvent proves that a workflow run records no event, because
// workflow code must replay the same way on every run.
func TestTemporal_C6_WorkflowEmitsNoEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	env := testEnvironment(t, log)

	job := func(workflow.Context) error { return nil }
	env.RegisterWorkflow(job)
	env.ExecuteWorkflow(job)

	if !env.IsWorkflowCompleted() {
		t.Fatal("the workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the workflow failed: %v", err)
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("workflow code recorded %d events, want none", count)
	}
}

// TestTemporal_C6_ActivityRecordsOneEvent proves that one activity attempt records one job
// event with the activity, the attempt, and the workflow fields.
func TestTemporal_C6_ActivityRecordsOneEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	env := testEnvironment(t, log)

	charge := func(ctx context.Context) (string, error) {
		return activity.GetInfo(ctx).ActivityType.Name, nil
	}
	env.RegisterActivity(charge)

	job := func(ctx workflow.Context) (string, error) {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{ScheduleToCloseTimeout: time.Minute})
		var name string
		if err := workflow.ExecuteActivity(ctx, charge).Get(ctx, &name); err != nil {
			return "", err
		}
		return name, nil
	}
	env.RegisterWorkflow(job)
	env.ExecuteWorkflow(job)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the workflow failed: %v", err)
	}
	var name string
	if err := env.GetWorkflowResult(&name); err != nil {
		t.Fatalf("the workflow returned no name: %v", err)
	}

	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want one activity event", len(events))
	}
	got := events[0]
	if got["kind"] != "job" || got["operation"] != "job "+name {
		t.Errorf("kind/operation = %v/%v, want job/job %s", got["kind"], got["operation"], name)
	}
	fields, _ := got["job"].(map[string]any)
	if fields["system"] != "temporal" || fields["name"] != name {
		t.Errorf("job = %v, want system temporal and name %s", fields, name)
	}
	if !conformance.Equal(fields["attempt"], 1) {
		t.Errorf("job.attempt = %v, want 1", fields["attempt"])
	}
	temporal, _ := fields["temporal"].(map[string]any)
	if temporal == nil || temporal["workflow_type"] == "" || temporal["workflow_id"] == "" {
		t.Errorf("job.temporal = %v, want the workflow type and id", fields["temporal"])
	}
}

// TestTemporal_C6_PanicRecordsStack proves that a panicking activity records one error event
// with a stack, and Temporal still sees the panic as a failed attempt.
func TestTemporal_C6_PanicRecordsStack(t *testing.T) {
	log, rec := wlogtest.New(t)
	env := testEnvironment(t, log)

	boom := func(context.Context) error { panic("boom") }
	env.RegisterActivity(boom)

	job := func(ctx workflow.Context) error {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{ScheduleToCloseTimeout: time.Minute})
		return workflow.ExecuteActivity(ctx, boom).Get(ctx, nil)
	}
	env.RegisterWorkflow(job)
	env.ExecuteWorkflow(job)

	if env.GetWorkflowError() == nil {
		t.Error("the workflow saw no error from the panicking activity")
	}
	got := lastEvent(t, rec)
	if got["outcome"] != "error" {
		t.Errorf("outcome = %v, want error", got["outcome"])
	}
	info, _ := got["error"].(map[string]any)
	if info == nil || info["stack"] == nil {
		t.Errorf("error = %v, want the recovered stack", got["error"])
	}
}

// TestTemporal_C6_ResultPendingIsNotAFailure proves that an activity which completes
// asynchronously records no error, and its result reaches Temporal.
func TestTemporal_C6_ResultPendingIsNotAFailure(t *testing.T) {
	log, rec := wlogtest.New(t)
	next := &activityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{
			Next: &pendingNext{err: activity.ErrResultPending},
		},
		log: log,
	}

	out, err := next.execute(context.Background(), activity.Info{
		ActivityType: activity.Type{Name: "charge"}, Attempt: 1,
	}, nil)
	if !errors.Is(err, activity.ErrResultPending) {
		t.Fatalf("execute returned %v, want the pending error", err)
	}
	if out != "done" {
		t.Errorf("execute returned %v, want the activity result", out)
	}
	got := lastEvent(t, rec)
	if got["level"] != "info" || got["outcome"] != "success" {
		t.Errorf("level/outcome = %v/%v, want info/success", got["level"], got["outcome"])
	}
	if _, present := got["error"]; present {
		t.Errorf("error = %v, want none for an asynchronous completion", got["error"])
	}
}

// TestTemporal_C1_UnitNamesTheAttempt proves the mapping from one activity info onto the
// fields of the table.
func TestTemporal_UnitNamesTheAttempt(t *testing.T) {
	scheduled := time.Now().Add(-2 * time.Second)
	unit := unitOf(activity.Info{
		ActivityID:        "act-1",
		ActivityType:      activity.Type{Name: "charge"},
		TaskQueue:         "payments",
		Attempt:           3,
		ScheduledTime:     scheduled,
		WorkflowType:      &workflow.Type{Name: "Checkout"},
		WorkflowExecution: workflow.Execution{ID: "wf-1"},
	})

	if unit.Kind != work.KindJob {
		t.Errorf("kind = %v, want job", unit.Kind)
	}
	if !unit.StartedAt.Equal(scheduled) {
		t.Errorf("started at = %v, want the scheduled time", unit.StartedAt)
	}
	for key, want := range map[string]any{
		"system": "temporal", "name": "charge", "id": "act-1",
		"queue": "payments", "attempt": 3,
	} {
		if !conformance.Equal(unit.Fields[key], want) {
			t.Errorf("job.%s = %v, want %v", key, unit.Fields[key], want)
		}
	}
	temporal, _ := unit.Fields["temporal"].(map[string]any)
	if temporal == nil || temporal["workflow_type"] != "Checkout" || temporal["workflow_id"] != "wf-1" {
		t.Errorf("job.temporal = %v, want Checkout/wf-1", unit.Fields["temporal"])
	}
}

// pendingNext is the next interceptor of a test, and it returns one fixed result.
type pendingNext struct {
	interceptor.ActivityInboundInterceptorBase
	err error
}

// ExecuteActivity returns the result the test set.
func (p pendingNext) ExecuteActivity(context.Context, *interceptor.ExecuteActivityInput) (any, error) {
	return "done", p.err
}

// testEnvironment builds a Temporal test environment with the wlog interceptor installed.
func testEnvironment(t *testing.T, log *wlog.Logger) *testsuite.TestWorkflowEnvironment {
	t.Helper()
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{Interceptor(log)},
	})
	return env
}

// lastEvent returns the only recorded event, and stops the test when the run recorded none.
func lastEvent(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	return got
}
