// Package sample provides wlog.Keeper implementations for head sampling (drop by rate per
// level) and tail sampling (force-keep on a rule), matching evlog's two-tier model. With
// no options, New keeps every event.
//
// A head decision follows the trace id, so every service that sees one trace keeps or drops
// it together, and a kept event records the rate that kept it in wlog.sample_rate.
package sample

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"

	"github.com/jeremygprawira/wlog"
)

// sampleBuckets is the resolution of a head decision. A rate is a percentage, so the
// resolution sets how fine a fractional rate can be: one million buckets resolves a
// thousandth of a percent.
const sampleBuckets = 1_000_000

// keeper holds the resolved rules of one sampler.
type keeper struct {
	rates []rateRule
	tails []tailRule
}

// rateRule is the head rate of one level.
type rateRule struct {
	level   wlog.Level
	percent float64
}

// Option configures a Keeper built by New.
type Option func(*keeper)

// New builds a wlog.Keeper from opts. With none, every event is kept.
//
// It returns an error for a rate outside 0 to 100, or for a glob that can never match,
// because a sampler that quietly kept everything is worse than one that will not start.
func New(opts ...Option) (wlog.Keeper, error) {
	k := &keeper{}
	for _, o := range opts {
		o(k)
	}
	for _, rule := range k.rates {
		if math.IsNaN(rule.percent) || rule.percent < 0 || rule.percent > 100 {
			return nil, fmt.Errorf("sample: rate %v for level %q is not a percentage between 0 and 100", rule.percent, rule.level)
		}
	}
	for _, tail := range k.tails {
		if err := tail.valid(); err != nil {
			return nil, err
		}
	}
	return k, nil
}

// MustNew is New, but panics on a bad rate or glob. Use it in a package-level variable.
func MustNew(opts ...Option) wlog.Keeper {
	k, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return k
}

// Rate sets the head-sampling percentage (0 to 100, fractions allowed) kept for one level. A
// level with no Rate call is kept whole.
func Rate(level wlog.Level, percent float64) Option {
	return func(k *keeper) { k.rates = append(k.rates, rateRule{level, percent}) }
}

// rateFor returns the rate of one level, the last Rate call for it winning.
func (k *keeper) rateFor(level wlog.Level) float64 {
	for i := len(k.rates) - 1; i >= 0; i-- {
		if k.rates[i].level == level {
			return k.rates[i].percent
		}
	}
	return 100
}

// Keep evaluates the rules in order: a tail rule the event matches force-keeps it, an error
// event is always kept, and every other event is decided by the head rate of its level.
func (k *keeper) Keep(ctx context.Context, event map[string]any) bool {
	for _, tail := range k.tails {
		if tail.matches(ctx, event) {
			recordRate(event, 100)
			return true
		}
	}
	if levelOf(event) == wlog.LevelError {
		// An error is never sampled away: a missing error is the one event a team needs.
		recordRate(event, 100)
		return true
	}
	return k.headKeep(event, k.rateFor(levelOf(event)))
}

// headKeep draws the head decision for one event.
//
// The draw hashes trace.trace_id when the event carries one, so the request, its retries, and
// every service that sees the same trace agree: either all of them keep it or none does. An
// event with no trace id is drawn at random from the rate.
func (k *keeper) headKeep(event map[string]any, percent float64) bool {
	if percent >= 100 {
		recordRate(event, 100)
		return true
	}
	if percent <= 0 {
		return false
	}

	kept := false
	if id := traceID(event); id != "" {
		kept = float64(hashID(id)%sampleBuckets) < percent*float64(sampleBuckets)/100
	} else {
		kept = rand.Float64()*100 < percent
	}
	if kept {
		recordRate(event, percent)
	}
	return kept
}

// recordRate stamps a kept event with the rate that kept it, so a reader can weigh a sampled
// count: a 100 means nothing sampled it away.
func recordRate(event map[string]any, percent float64) {
	event["wlog.sample_rate"] = percent
}

// levelOf reads an event's level.
func levelOf(event map[string]any) wlog.Level {
	level, _ := event["level"].(string)
	return wlog.Level(level)
}

// traceID reads trace.trace_id.
func traceID(event map[string]any) string {
	trace, ok := event["trace"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := trace["trace_id"].(string)
	return id
}

// hashID hashes a trace id, so the same id always lands in the same bucket.
func hashID(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}
