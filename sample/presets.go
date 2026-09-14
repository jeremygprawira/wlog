package sample

import "time"

// KeepErrorsAndSlow is the common production shape: every error is force-kept (via
// Keep's own always-keep-errors rule), every event at least slow milliseconds long is
// force-kept, and everything else is head-sampled at healthyRate percent.
func KeepErrorsAndSlow(slow time.Duration, healthyRate int) Option {
	return func(k *keeper) {
		KeepDuration(slow)(k)
		Rate("info", healthyRate)(k)
		Rate("debug", healthyRate)(k)
		Rate("warn", healthyRate)(k)
	}
}
