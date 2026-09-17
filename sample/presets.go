package sample

import (
	"context"
	"time"

	"github.com/jeremygprawira/wlog"
)

// KeepErrorsAndSlow is the common production shape: every warn and error is force-kept
// (Keep's own rule covers an error), every 5xx is force-kept because the status is the
// failure even when the handler logged no error, every event at least slow milliseconds long
// is force-kept, every info event is head-sampled at healthyRate percent, and a healthy debug
// line is not kept at all.
//
// A 5xx and a warn line are the two events a team reads first, so neither is left to a rate.
func KeepErrorsAndSlow(slow time.Duration, healthyRate float64) Option {
	return func(k *keeper) {
		KeepDuration(slow)(k)
		KeepStatus(500)(k)
		KeepFunc(func(_ context.Context, event map[string]any) bool {
			return levelOf(event) == wlog.LevelWarn
		})(k)
		Rate(wlog.LevelInfo, healthyRate)(k)
		Rate(wlog.LevelDebug, 0)(k)
	}
}
