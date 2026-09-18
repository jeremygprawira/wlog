// This file holds the ticker: one job event per tick of a clock, which is how a worker, a
// reaper, or a poller reports what it did.
package work

import (
	"context"
	"time"

	"github.com/jeremygprawira/wlog"
)

// Ticker runs fn on each tick of a time.Ticker as one job event of system ticker, until
// ctx ends. A tick that arrives while the previous run is still going is skipped, and the
// next event records the skip under job.ticker.skipped.
//
// A panic in fn is recorded with its stack, the event emits, and the panic continues.
//
// ponytail: a time.Ticker holds one pending tick and drops the rest, so a run that
// overruns several ticks records one skip. Track the tick time to count them all.
func Ticker(ctx context.Context, log *wlog.Logger, name string, d time.Duration, fn func(context.Context) error) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()

	skipped := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		runTick(ctx, log, name, skipped, fn)
		// A tick that arrived while the run was going is the one the runtime held.
		skipped = pendingTicks(ticker)
	}
}

// runTick runs one tick as one job event. It records the skip of the run before it, and it
// re-raises a panic once the event is out.
func runTick(ctx context.Context, log *wlog.Logger, name string, skipped int, fn func(context.Context) error) {
	unit := Unit{Kind: KindJob, Fields: map[string]any{"system": "ticker", "name": name}}
	_, h := Start(ctx, log, unit)
	if skipped > 0 {
		h.Set("ticker", map[string]any{"skipped": skipped})
	}

	recovered, err := callSafe(ctx, fn)
	h.End(err)
	if recovered != nil {
		panic(recovered)
	}
}

// pendingTicks returns the number of ticks already waiting, which is 1 while the run of
// this ticker is still going.
func pendingTicks(ticker *time.Ticker) int {
	select {
	case <-ticker.C:
		return 1
	default:
		return 0
	}
}
