// This file holds the task middleware, the plain-handler wrapper, the result rule, and the
// mapping from one task onto one unit of work.
package wlogasynq

import (
	"context"
	"errors"

	"github.com/hibiken/asynq"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Middleware returns asynq middleware that gives every processed task one event. Add it with
// ServeMux.Use, or use Handler for Server.Run without a mux. A handler that panics records
// the panic with a stack, emits the event, and panics again, so asynq keeps its own panic
// behavior. A nil Logger means wlog.Default.
func Middleware(log *wlog.Logger) asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
			return run(ctx, log, task, func(ctx context.Context) error {
				return next.ProcessTask(ctx, task)
			})
		})
	}
}

// Handler wraps one handler for Server.Run, which runs a plain handler and applies no
// ServeMux middleware. Handler covers for that server what Middleware covers for a mux. A
// nil Logger means wlog.Default.
func Handler(log *wlog.Logger, h asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		return run(ctx, log, task, func(ctx context.Context) error {
			return h.ProcessTask(ctx, task)
		})
	})
}

// run opens one job event around the handler of one task, records the state asynq keeps for
// the task, and ends the event. It returns the error of the handler.
//
// A panic is recorded in error with its stack, and no result, because asynq decides the state
// of a panicking task after the middleware returns.
func run(ctx context.Context, log *wlog.Logger, task *asynq.Task, handler func(context.Context) error) error {
	return work.Run(ctx, log, unitOf(ctx, task), func(ctx context.Context) error {
		err := handler(ctx)
		if result, level := stateOf(ctx, err); result != "" {
			wlog.SetGroup(ctx, "job", "result", result)
			if level != "" {
				wlog.SetLevel(ctx, level)
			}
		}
		return err
	})
}

// stateOf names the state asynq will keep for one task, and the level that state asks for.
// asynq decides after the handler returns, so the middleware reads the retry counts of the
// context. RevokeTask cancels the task, SkipRetry and an exhausted retry count discard it,
// and any other error retries it.
func stateOf(ctx context.Context, err error) (string, wlog.Level) {
	if err == nil {
		return "", ""
	}
	switch {
	case errors.Is(err, asynq.RevokeTask):
		return "cancel", wlog.LevelWarn
	case errors.Is(err, asynq.SkipRetry):
		return "discard", ""
	}
	retried, ok := asynq.GetRetryCount(ctx)
	if !ok {
		return "retry", ""
	}
	if max, ok := asynq.GetMaxRetry(ctx); ok && retried >= max {
		return "discard", ""
	}
	return "retry", ""
}

// unitOf maps one processing task onto a unit of work. asynq puts the id, the queue, and the
// retry counts on the context, and the task headers carry the trace context of the producer.
func unitOf(ctx context.Context, task *asynq.Task) work.Unit {
	fields := map[string]any{"system": "asynq", "name": task.Type()}
	if id, ok := asynq.GetTaskID(ctx); ok && id != "" {
		fields["id"] = id
	}
	if queue, ok := asynq.GetQueueName(ctx); ok && queue != "" {
		fields["queue"] = queue
	}
	if retried, ok := asynq.GetRetryCount(ctx); ok {
		fields["attempt"] = retried + 1
	}
	if max, ok := asynq.GetMaxRetry(ctx); ok {
		// asynq counts the retries after the first attempt, and the event names attempts.
		fields["max_attempts"] = max + 1
	}
	return work.Unit{
		Kind:    work.KindJob,
		Fields:  fields,
		Carrier: propagate.MapCarrier(task.Headers()),
	}
}
