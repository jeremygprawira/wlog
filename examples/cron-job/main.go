// Command cron-job runs one scheduled reindex with wlog around every run. It is the cron-job
// recipe's example: one event per run, with the name and the schedule, and a flush when the
// scheduler stops.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"

	"github.com/jeremygprawira/wlog"
	wlogcron "github.com/jeremygprawira/wlog/job/cron"
)

// spec is the schedule of the job.
const spec = "@every 5m"

// reindex is the job itself. The handler adds the field a searcher asks for.
func reindex(ctx context.Context) error {
	wlog.Set(ctx, "rows", 128)
	return nil
}

// newScheduler builds the scheduler with one event per run. Job opens the event, and
// SkipIfStillRunning sits outside it, so a skipped run records nothing. Wrap covers a plain
// cron.Job; use one of the two, never both, because each opens its own event.
func newScheduler(logger *wlog.Logger) *cron.Cron {
	c := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger)))
	_, _ = c.AddJob(spec, wlogcron.Job(logger, "reindex", spec, reindex))
	return c
}

func main() {
	logger := wlog.New(wlog.WithService("cron-job", "0.0.1", "local"))
	scheduler := newScheduler(logger)
	scheduler.Start()

	// Stop returns a context that is done when the running jobs finish. A short-lived
	// process flushes there, because it ends soon after.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	<-scheduler.Stop().Done()
	_ = logger.Flush(context.Background())
}
