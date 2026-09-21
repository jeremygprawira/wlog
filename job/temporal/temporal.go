// This file holds the worker interceptor, the activity interceptor, and the mapping from
// one activity attempt onto one unit of work.
package wlogtemporal

import (
	"context"
	"errors"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// Interceptor returns the Temporal worker interceptor that gives every activity attempt one
// event. Set it in worker.Options.Interceptors. Workflow code emits nothing, because a
// workflow must replay the same way on every run. A nil Logger means wlog.Default.
func Interceptor(log *wlog.Logger) interceptor.WorkerInterceptor {
	return &workerInterceptor{log: log}
}

// workerInterceptor wraps activities and leaves workflow and Nexus calls untouched.
type workerInterceptor struct {
	interceptor.WorkerInterceptorBase
	log *wlog.Logger
}

// InterceptActivity wraps one activity interceptor in a job event.
func (w *workerInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &activityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
		log:                            w.log,
	}
}

// activityInterceptor gives every activity attempt one event.
type activityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
	log *wlog.Logger
}

// ExecuteActivity opens one job event around the activity attempt, and returns its result.
// An activity that completes asynchronously is not a failure, so its event stays successful.
// A panic records the panic with a stack, emits the event, and panics again, so Temporal
// keeps its own panic behavior.
func (a *activityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (any, error) {
	return a.execute(ctx, activity.GetInfo(ctx), in)
}

// execute runs one activity attempt inside its event, with the activity info the caller
// reads. It is separate from ExecuteActivity, so a test drives the event path with an
// activity info it builds.
func (a *activityInterceptor) execute(ctx context.Context, info activity.Info, in *interceptor.ExecuteActivityInput) (any, error) {
	var out any
	var activityErr error
	eventErr := work.Run(ctx, a.log, unitOf(info), func(ctx context.Context) error {
		out, activityErr = a.Next.ExecuteActivity(ctx, in)
		return failureOf(activityErr)
	})
	if eventErr != nil {
		return out, eventErr
	}
	return out, activityErr
}

// process runs one unit of work through the event path of this adapter, with a recovered
// panic as an error, so a test continues after the panic scenario.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// failureOf returns the error the event records for one activity result. An asynchronous
// completion is not a failure, so it records none, and the activity keeps its own result.
func failureOf(err error) error {
	if errors.Is(err, activity.ErrResultPending) {
		return nil
	}
	return err
}

// unitOf maps one activity attempt onto a unit of work. The scheduled time becomes the start
// time, so the event carries the time the activity waited as lag_ms. The workflow type and
// id go under job.temporal, because the work table has no field for them.
func unitOf(info activity.Info) work.Unit {
	fields := map[string]any{"system": "temporal", "name": info.ActivityType.Name}
	if info.ActivityID != "" {
		fields["id"] = info.ActivityID
	}
	if info.TaskQueue != "" {
		fields["queue"] = info.TaskQueue
	}
	if info.Attempt > 0 {
		fields["attempt"] = int(info.Attempt)
	}
	workflowType := ""
	if info.WorkflowType != nil {
		workflowType = info.WorkflowType.Name
	}
	if info.WorkflowExecution.ID != "" || workflowType != "" {
		fields["temporal"] = map[string]any{
			"workflow_id":   info.WorkflowExecution.ID,
			"workflow_type": workflowType,
		}
	}
	return work.Unit{
		Kind:      work.KindJob,
		Fields:    fields,
		StartedAt: info.ScheduledTime,
	}
}
