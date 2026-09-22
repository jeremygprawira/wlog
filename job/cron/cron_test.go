// This file runs the work conformance suite against the cron path, and checks the fields,
// the error, and the panic rule of the wrapper.
package wlogcron

import (
	"context"
	"testing"

	"github.com/robfig/cron/v3"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestCron_C1_WorkConformance proves that the cron path passes every scenario of the work
// suite.
func TestCron_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path the wrapper uses. The suite supplies the
// unit, because one cron run carries no rpc, message, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestCron_C1_WrapRecordsTheSchedule proves that one wrapped run records the job group with
// the name and the schedule.
func TestCron_WrapRecordsTheSchedule(t *testing.T) {
	log, rec := wlogtest.New(t)
	ran := false
	job := Wrap(log, "reindex", "@every 5m")(cron.FuncJob(func() { ran = true }))

	job.Run()

	if !ran {
		t.Fatal("the wrapped job did not run")
	}
	checkEvent(t, lastEvent(t, rec), "reindex", "@every 5m")
}

// TestCron_C1_JobRecordsTheSchedule proves that one Job run records the job group with the
// name and the schedule, and gives the function a context.
func TestCron_JobRecordsTheSchedule(t *testing.T) {
	log, rec := wlogtest.New(t)
	got := false
	job := Job(log, "reindex", "0 * * * *", func(ctx context.Context) error {
		got = ctx != nil
		return nil
	})

	job.Run()

	if !got {
		t.Error("the function saw no context")
	}
	checkEvent(t, lastEvent(t, rec), "reindex", "0 * * * *")
}

// TestCron_C1_ErrorRecordsError proves that a run whose function fails records level and
// outcome error with the message of the error.
func TestCron_ErrorRecordsError(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := Job(log, "reindex", "@every 5m", func(context.Context) error { return errString("boom") })

	job.Run()

	got := lastEvent(t, rec)
	if got["level"] != "error" || got["outcome"] != "error" {
		t.Errorf("level/outcome = %v/%v, want error/error", got["level"], got["outcome"])
	}
	info, _ := got["error"].(map[string]any)
	if info == nil || info["message"] != "boom" {
		t.Errorf("error = %v, want the error of the function", got["error"])
	}
}

// TestCron_C1_PanicRecordsStackAndPanics proves that a panicking run records one error event
// with a stack, and the wrapper panics again, so cron keeps its own panic behavior.
func TestCron_PanicRecordsStackAndPanics(t *testing.T) {
	log, rec := wlogtest.New(t)
	job := Job(log, "reindex", "@every 5m", func(context.Context) error { panic("boom") })

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("the wrapper did not panic again")
			}
		}()
		job.Run()
	}()

	info, _ := lastEvent(t, rec)["error"].(map[string]any)
	if info == nil || info["stack"] == nil {
		t.Errorf("error = %v, want the recovered stack", info)
	}
}

// checkEvent proves the job group of one run.
func checkEvent(t *testing.T, got map[string]any, name, spec string) {
	t.Helper()
	if got["kind"] != "job" || got["operation"] != "job "+name {
		t.Errorf("kind/operation = %v/%v, want job/job %s", got["kind"], got["operation"], name)
	}
	fields, _ := got["job"].(map[string]any)
	for key, want := range map[string]any{"system": "cron", "name": name, "schedule": spec} {
		if fields[key] != want {
			t.Errorf("job.%s = %v, want %v", key, fields[key], want)
		}
	}
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

// errString is the plain error a test returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
