// This file holds the producer side: one call per enqueue, and the trace header of the
// context.
package wlogasynq

import (
	"context"

	"github.com/hibiken/asynq"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Client is the part of *asynq.Client that Enqueue uses. *asynq.Client satisfies it, and a
// test fake does too, because no test can reach a Redis server.
type Client interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// Enqueue builds one task with the trace headers of ctx, enqueues it through client, and
// records one call on the event of ctx. Build the task here and not with asynq.NewTask,
// because asynq has no header setter and the task headers carry the trace context.
func Enqueue(ctx context.Context, client Client, typename string, payload []byte, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "asynq", Operation: "enqueue", Target: typename,
	})
	task := asynq.NewTaskWithHeaders(typename, payload, traceHeaders(ctx), opts...)
	out, err := client.EnqueueContext(ctx, task)
	end(resultOf(err))
	return out, err
}

// traceHeaders returns the trace headers of ctx as a plain map, and nil when ctx carries no
// trace context.
func traceHeaders(ctx context.Context) map[string]string {
	if _, ok := propagate.FromContext(ctx); !ok {
		return nil
	}
	carrier := propagate.MapCarrier{}
	propagate.Inject(ctx, carrier)
	return map[string]string(carrier)
}

// resultOf builds the call result of one finished enqueue.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
