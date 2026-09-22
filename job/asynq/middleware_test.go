// This file runs the work conformance suite against the task path, and checks the fields,
// the result, and the panic rule of the middleware.
package wlogasynq

import (
	"context"
	"fmt"
	"errors"
	"testing"

	"github.com/hibiken/asynq"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestAsynq_C1_WorkConformance proves that the task path passes every scenario of the work
// suite.
func TestAsynq_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory drives the real middleware with one task, so a change that breaks the adapter
// fails the suite.
type workFactory struct{}

// Declare names the one kind an asynq task produces. The retry counts of a task live in a
// context that only asynq builds, so the suite skips the attempt case.
func (workFactory) Declare() workconformance.Declaration {
	return workconformance.Declaration{Kinds: []work.Kind{work.KindJob}}
}

// Process runs one unit of work through Middleware. The suite expects no panic from Process,
// so the panic of the handler, which the middleware raises again, comes back as an error.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	name, _ := unit.Fields["name"].(string)
	task := asynq.NewTask(name, nil)
	h := Middleware(log)(asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		return handler(ctx)
	}))
	return h.ProcessTask(context.Background(), task)
}

// TestAsynq_C1_EventNamesTheTask proves that one processed task records the job group with
// the system and the task type, and the operation of the kind.
func TestAsynq_EventNamesTheTask(t *testing.T) {
	log, rec := wlogtest.New(t)

	if err := processTask(t, log, "reindex:orders", succeed); err != nil {
		t.Fatalf("ProcessTask returned %v", err)
	}
	got := lastEvent(t, rec)
	if got["operation"] != "job reindex:orders" {
		t.Errorf("operation = %v, want job reindex:orders", got["operation"])
	}
	job, _ := got["job"].(map[string]any)
	if job["system"] != "asynq" || job["name"] != "reindex:orders" {
		t.Errorf("job = %v, want system asynq and name reindex:orders", job)
	}
}

// TestAsynq_C1_SkipRetryRecordsDiscard proves that a handler that returns SkipRetry records
// result discard and keeps the level of the error.
func TestAsynq_SkipRetryRecordsDiscard(t *testing.T) {
	log, rec := wlogtest.New(t)

	err := processTask(t, log, "reindex", func(context.Context, *asynq.Task) error { return asynq.SkipRetry })
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("ProcessTask returned %v, want the handler error", err)
	}
	got := lastEvent(t, rec)
	if got["level"] != "error" || got["outcome"] != "error" {
		t.Errorf("level/outcome = %v/%v, want error/error", got["level"], got["outcome"])
	}
	if result := jobField(t, got, "result"); result != "discard" {
		t.Errorf("job.result = %v, want discard", result)
	}
}

// TestAsynq_C1_RevokeRecordsCancel proves that a handler that returns RevokeTask records
// result cancel and level warn.
func TestAsynq_RevokeRecordsCancel(t *testing.T) {
	log, rec := wlogtest.New(t)

	_ = processTask(t, log, "reindex", func(context.Context, *asynq.Task) error { return asynq.RevokeTask })

	got := lastEvent(t, rec)
	if got["level"] != "warn" {
		t.Errorf("level = %v, want warn", got["level"])
	}
	if result := jobField(t, got, "result"); result != "cancel" {
		t.Errorf("job.result = %v, want cancel", result)
	}
}

// TestAsynq_C1_PlainErrorRecordsRetry proves that a handler error with no retry data records
// result retry.
func TestAsynq_PlainErrorRecordsRetry(t *testing.T) {
	log, rec := wlogtest.New(t)

	_ = processTask(t, log, "reindex", func(context.Context, *asynq.Task) error { return errString("boom") })

	if result := jobField(t, lastEvent(t, rec), "result"); result != "retry" {
		t.Errorf("job.result = %v, want retry", result)
	}
}

// TestAsynq_C1_PanicRecordsStackAndPanics proves that a panicking handler records one error
// event with a stack, and the middleware panics again, so asynq keeps its own panic behavior.
func TestAsynq_PanicRecordsStackAndPanics(t *testing.T) {
	log, rec := wlogtest.New(t)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("the middleware did not panic again")
			}
		}()
		_ = processTask(t, log, "reindex", func(context.Context, *asynq.Task) error { panic("boom") })
	}()

	got := lastEvent(t, rec)
	if got["outcome"] != "error" {
		t.Errorf("outcome = %v, want error", got["outcome"])
	}
	info, _ := got["error"].(map[string]any)
	if info == nil || info["stack"] == nil {
		t.Errorf("error = %v, want the recovered stack", got["error"])
	}
}

// TestAsynq_C1_HandlerCoversAServerWithoutAMux proves that Handler wraps a plain handler, so
// a server that runs no ServeMux still gives every task one event.
func TestAsynq_HandlerCoversAServerWithoutAMux(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := Handler(log, asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil }))

	if err := h.ProcessTask(context.Background(), asynq.NewTask("reindex", nil)); err != nil {
		t.Fatalf("ProcessTask returned %v", err)
	}
	if got := lastEvent(t, rec); got["kind"] != "job" {
		t.Errorf("kind = %v, want job", got["kind"])
	}
}

// processTask runs one task through Middleware with the given handler.
func processTask(t *testing.T, log *wlog.Logger, typename string, h asynq.HandlerFunc) error {
	t.Helper()
	return Middleware(log)(h).ProcessTask(context.Background(), asynq.NewTask(typename, nil))
}

// succeed is the handler of a task that finishes without an error.
func succeed(context.Context, *asynq.Task) error { return nil }

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
