// This file holds the worker middleware, the result rule, and the mapping from one job onto
// one unit of work.
package wlogriver

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Middleware is the River worker middleware that gives every worked job one event. Set it in
// Config.WorkerMiddleware, or in Config.Middleware on a newer River, because the type has
// both Work and IsMiddleware.
type Middleware struct {
	log *wlog.Logger
}

// New returns the worker middleware that gives every worked job one event. A nil Logger
// means wlog.Default.
func New(log *wlog.Logger) *Middleware { return &Middleware{log: log} }

// IsMiddleware marks the type as River middleware. A newer River asks for this method, so
// the same value works in Config.Middleware and in Config.WorkerMiddleware.
func (*Middleware) IsMiddleware() bool { return true }

// Work opens one job event around the job, records the state River keeps for it, and returns
// the error of the job. A job that panics records the panic with a stack, emits the event,
// and panics again, so River keeps its own panic behavior.
func (m *Middleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	return work.Run(ctx, m.log, unitOf(job), func(ctx context.Context) error {
		err := doInner(ctx)
		if result, level := stateOf(job, err); result != "" {
			wlog.SetGroup(ctx, "job", "result", result)
			if level != "" {
				wlog.SetLevel(ctx, level)
			}
		}
		return err
	})
}

// process runs one unit of work through the event path of this adapter, with a recovered
// panic as an error, so a test continues after the panic scenario.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// stateOf names the state River will keep for one job, and the level that state asks for.
// River decides after the middleware returns, so the middleware reads the error and the
// attempt. JobSnooze snoozes the job, JobCancel cancels it, an error on the last attempt
// discards it, and any other error retries it.
func stateOf(job *rivertype.JobRow, err error) (string, wlog.Level) {
	if err == nil {
		return "", ""
	}
	switch {
	case errors.Is(err, &river.JobSnoozeError{}):
		return "snooze", wlog.LevelInfo
	case errors.Is(err, &river.JobCancelError{}):
		return "cancel", wlog.LevelWarn
	}
	if job != nil && job.Attempt >= job.MaxAttempts {
		return "discard", ""
	}
	return "retry", ""
}

// unitOf maps one worked job onto a unit of work. The scheduled time becomes the start time,
// so the event carries the time the job waited as lag_ms, and the metadata carries the trace
// context of the inserter.
func unitOf(job *rivertype.JobRow) work.Unit {
	fields := map[string]any{"system": "river"}
	if job == nil {
		return work.Unit{Kind: work.KindJob, Fields: fields}
	}
	fields["name"] = job.Kind
	if job.ID != 0 {
		fields["id"] = strconv.FormatInt(job.ID, 10)
	}
	if job.Queue != "" {
		fields["queue"] = job.Queue
	}
	if job.Attempt > 0 {
		fields["attempt"] = job.Attempt
	}
	if job.MaxAttempts > 0 {
		fields["max_attempts"] = job.MaxAttempts
	}
	return work.Unit{
		Kind:      work.KindJob,
		Fields:    fields,
		Carrier:   metadataCarrier(job.Metadata),
		StartedAt: job.ScheduledAt,
	}
}

// metadataCarrier reads the trace headers of one job metadata JSON object. River stores
// metadata as a JSON object, so the reader takes its string values. Bad JSON, or metadata
// with no string value, reads as no carrier.
func metadataCarrier(metadata []byte) propagate.Carrier {
	if len(metadata) == 0 {
		return nil
	}
	fields := map[string]any{}
	if err := json.Unmarshal(metadata, &fields); err != nil {
		return nil
	}
	values := propagate.MapCarrier{}
	for key, value := range fields {
		if text, ok := value.(string); ok {
			values[key] = text
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}
