// Package sample provides wlog.Keeper implementations for head sampling (drop by
// random rate per level) and tail sampling (force-keep on outcome), matching evlog's
// two-tier model. With no options, New keeps every event.
package sample

import (
	"context"
	"math/rand"

	"github.com/jeremygprawira/wlog"
)

type keeper struct {
	rates []rateRule
	tails []func(ctx context.Context, event map[string]any) bool
}

type rateRule struct {
	level   wlog.Level
	percent int
}

// Option configures a Keeper built by New.
type Option func(*keeper)

// New builds a wlog.Keeper from opts. With none, every event is kept.
func New(opts ...Option) wlog.Keeper {
	k := &keeper{}
	for _, o := range opts {
		o(k)
	}
	return k
}

// Rate sets the head-sampling percentage (0-100) kept for one level. A level with no
// Rate call defaults to 100 (kept). LevelError defaults to 100 too, but unlike other
// levels it is always force-kept regardless of any Rate set for it (see Keep).
func Rate(level wlog.Level, percent int) Option {
	return func(k *keeper) { k.rates = append(k.rates, rateRule{level, percent}) }
}

func (k *keeper) rateFor(level wlog.Level) int {
	for i := len(k.rates) - 1; i >= 0; i-- {
		if k.rates[i].level == level {
			return k.rates[i].percent
		}
	}
	return 100
}

// Keep evaluates, in order: any tail rule (OR-combined) force-keeps the event; a
// LevelError event is always force-kept; otherwise head sampling draws against the
// level's rate.
func (k *keeper) Keep(ctx context.Context, event map[string]any) bool {
	for _, tail := range k.tails {
		if tail(ctx, event) {
			return true
		}
	}
	if level, _ := event["level"].(string); wlog.Level(level) == wlog.LevelError {
		return true
	}
	level, _ := event["level"].(string)
	percent := k.rateFor(wlog.Level(level))
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	return rand.Intn(100) < percent
}
