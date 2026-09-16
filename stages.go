package wlog

import (
	"context"
	"fmt"
	"reflect"
)

// Keeper decides whether to keep an event that would otherwise be sampled away. It is
// the hook point for head/tail sampling (a later module); with none configured, every
// event is kept.
type Keeper interface {
	Keep(ctx context.Context, event map[string]any) bool
}

// KeeperFunc adapts a plain function to the Keeper interface.
type KeeperFunc func(ctx context.Context, event map[string]any) bool

// Keep calls f.
func (f KeeperFunc) Keep(ctx context.Context, event map[string]any) bool { return f(ctx, event) }

// Enricher adds derived fields to an event: host info, a parsed user agent, geo, and
// so on (the enrich module). Enrichers run after sampling decides to keep an event and
// before redaction, so anything an enricher adds is still masked like any other field.
type Enricher interface {
	Enrich(ctx context.Context, event map[string]any)
}

// EnricherFunc adapts a plain function to the Enricher interface.
type EnricherFunc func(ctx context.Context, event map[string]any)

// Enrich calls f.
func (f EnricherFunc) Enrich(ctx context.Context, event map[string]any) { f(ctx, event) }

// WithSampler sets the Keeper used to decide which events to keep. Unset, every event
// is kept.
func WithSampler(k Keeper) Option {
	return func(l *Logger) { l.sampler = k }
}

// WithEnrichers adds enrichers, run in order, after sampling and before redaction.
func WithEnrichers(enrichers ...Enricher) Option {
	return func(l *Logger) { l.enrichers = append(l.enrichers, enrichers...) }
}

// shouldKeep runs the fixed keep/sample stage: an event carrying the reserved "audit"
// key always bypasses sampling (SPEC.md: audit is never sampled away); otherwise a
// configured Keeper decides, panic-isolated, falling back to "keep" so a broken
// sampler can never silently drop every event.
func (l *Logger) shouldKeep(ctx context.Context, event map[string]any) bool {
	if _, isAudit := event["audit"]; isAudit {
		return true
	}
	if l.sampler == nil {
		return true
	}
	return l.safeKeep(ctx, event)
}

func (l *Logger) safeKeep(ctx context.Context, event map[string]any) (keep bool) {
	keep = true
	defer func() {
		if r := recover(); r != nil {
			l.reportError(fmt.Errorf("panic: %v", r), sourceName(l.sampler))
			keep = true
		}
	}()
	return l.sampler.Keep(ctx, event)
}

// runEnrichers runs the enrich stage: every configured Enricher, in order, each
// panic-isolated so one bad enricher never drops the fields the others added or the
// event itself.
func (l *Logger) runEnrichers(ctx context.Context, event map[string]any) {
	before := make(map[string]any, len(event))
	for key, value := range event {
		before[key] = value
	}
	for _, en := range l.enrichers {
		l.safeEnrich(ctx, en, event)
	}
	// An enricher writes into the canonical map, which core handed to it. Copy
	// what it added or changed before the redactor and the drains run, so core
	// owns what it emits and an enricher that keeps a reference can never change
	// the event after this point. The fields core itself wrote stay as they are,
	// so a reserved field keeps its own type.
	for key, value := range event {
		if enriched(before[key], value) {
			event[key] = copyValue(value)
		}
	}
}

// enriched reports whether an enricher added or changed a value.
//
// The comparison looks inside a map or a slice, because an enricher may write
// through the reference core gave it rather than replace the field. A field that
// core itself wrote, such as the list of unknown keys, compares equal and keeps
// its own type.
func enriched(before, after any) bool {
	return !reflect.DeepEqual(before, after)
}

func (l *Logger) safeEnrich(ctx context.Context, en Enricher, event map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			l.reportError(fmt.Errorf("panic: %v", r), sourceName(en))
		}
	}()
	en.Enrich(ctx, event)
}
