// This file holds the job wrapper, the job builder, and the mapping from one run onto one
// unit of work.
package wlogcron

import (
	"context"

	"github.com/robfig/cron/v3"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// Wrap returns the job wrapper that gives every run of a job one event. name and spec fill
// the job group, because cron gives the wrapper neither the entry id nor the schedule. Place
// Wrap inside cron.SkipIfStillRunning, so a skipped run records nothing.
//
// Wrap names every job of the scheduler it wraps with one name and one schedule. Use Wrap for
// a plain cron.Job, and Job for one function with a context and an error. Never combine the
// two, because each one opens its own event.
//
// The flush point of a short-lived process is the context that Stop returns. A panic reaches
// the caller before any flush, so a process that ends on a panic loses its pending events. A
// nil Logger means wlog.Default.
func Wrap(log *wlog.Logger, name, spec string) cron.JobWrapper {
	return func(next cron.Job) cron.Job {
		return cron.FuncJob(func() {
			_ = work.Run(context.Background(), log, unitOf(name, spec), func(context.Context) error {
				next.Run()
				return nil
			})
		})
	}
}

// Job returns a cron job that runs fn with a context and gives every run one event. name and
// spec fill the job group. A panic records the panic with a stack, emits the event, and
// panics again, so cron keeps its own panic behavior. A nil Logger means wlog.Default.
func Job(log *wlog.Logger, name, spec string, fn func(ctx context.Context) error) cron.Job {
	return cron.FuncJob(func() {
		_ = work.Run(context.Background(), log, unitOf(name, spec), fn)
	})
}

// process runs one unit of work through the event path of this adapter, with a recovered
// panic as an error, so a test continues after the panic scenario.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// unitOf maps one run onto a unit of work.
func unitOf(name, spec string) work.Unit {
	return work.Unit{
		Kind:   work.KindJob,
		Fields: map[string]any{"system": "cron", "name": name, "schedule": spec},
	}
}
