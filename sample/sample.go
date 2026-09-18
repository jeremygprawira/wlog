// Package sample provides wlog.HeadSampler and wlog.Keeper implementations, matching
// evlog's two-tier model. Sample makes the head decision (drop by rate per level) and
// Keep makes the tail decision (force-keep on a rule).
//
// A head decision follows the trace id, so every service that sees one trace keeps or
// drops it together. New returns one value that implements both interfaces, so a caller
// passes it to wlog.WithHeadSampler and to wlog.WithKeepers.
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

// Option configures a Sampler built by New.
type Option func(*keeper)

// Sampler makes both sampling decisions: Sample draws the head rate for one level, and
// Keep force-keeps an enriched event on a tail rule.
type Sampler interface {
	wlog.HeadSampler
	wlog.Keeper
}

// New builds a Sampler from opts, and reports an error for a rate outside 0 to 100 or
// for a glob that can never match. A sampler that quietly kept everything is worse than
// one that will not start.
func New(opts ...Option) (Sampler, error) {
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
func MustNew(opts ...Option) Sampler {
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

// Sample makes the head decision for one level, and reports the rate that decided it.
// Core records that rate on the event, so a reader can weigh a sampled count.
//
// The draw hashes the trace id when the event carries one, so the request, its retries,
// and every service that sees the same trace agree: either all of them keep it or none
// does. An event with no trace id is drawn at random from the rate.
func (k *keeper) Sample(level wlog.Level, traceID string) (keep bool, rate float64) {
	percent := k.rateFor(level)
	switch {
	case percent >= 100:
		return true, 100
	case percent <= 0:
		return false, 0
	}
	if traceID == "" {
		return rand.Float64()*100 < percent, percent
	}
	return float64(hashID(traceID)%sampleBuckets) < percent*float64(sampleBuckets)/100, percent
}

// Keep force-keeps the events a team needs most: one that matches a tail rule, and an
// error-level event.
//
// It reads the enriched event through the read-only view, so a tail rule matches the
// fields a reader would see. It never drops an event, because the head decision owns
// that answer.
func (k *keeper) Keep(ctx context.Context, event wlog.Event) bool {
	for _, tail := range k.tails {
		if tail.matches(ctx, event) {
			return true
		}
	}
	// An error is never sampled away: a missing error is the one event a team needs.
	return event.Level() == wlog.LevelError
}

// hashID hashes a trace id, so the same id always lands in the same bucket.
func hashID(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}
