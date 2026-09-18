package wlog

import (
	"context"
	"fmt"
	"reflect"
	"sort"
)

// HeadSampler makes the head decision from the level and the trace id alone, before any
// field is added to the event. It returns whether to keep the event and the rate that
// decided, so core can record the rate on an event a later Keeper forces back.
type HeadSampler interface {
	Sample(level Level, traceID string) (keep bool, rate float64)
}

// Keeper decides on the enriched event, right after the enrich stage. It returns true
// to force the event back from a head drop, and false to leave the head decision as it
// is. It never drops an event the head sampler kept, so one Keeper that says yes is
// enough to keep it.
type Keeper interface {
	Keep(ctx context.Context, event Event) bool
}

// KeeperFunc adapts a plain function to the Keeper interface.
type KeeperFunc func(ctx context.Context, event Event) bool

// Keep calls f.
func (f KeeperFunc) Keep(ctx context.Context, event Event) bool { return f(ctx, event) }

// Enricher adds derived fields to an event: host info, a parsed user agent, geo, and
// so on (the enrich module). Enrichers run after the head decision and before the tail
// keep, so a Keeper reads the fields an enricher added.
type Enricher interface {
	Enrich(ctx context.Context, event map[string]any)
}

// EnricherFunc adapts a plain function to the Enricher interface.
type EnricherFunc func(ctx context.Context, event map[string]any)

// Enrich calls f.
func (f EnricherFunc) Enrich(ctx context.Context, event map[string]any) { f(ctx, event) }

// WithHeadSampler sets the head sampler, which decides which events to build. It sees
// the level and the trace id only. Unset, every event is built.
func WithHeadSampler(s HeadSampler) Option {
	return func(l *Logger) { l.headSampler = s }
}

// WithKeepers adds keepers, run in order after the enrich stage. Several keepers, from
// this option and from plugins, all run, and one keeper that says yes keeps the event.
func WithKeepers(keepers ...Keeper) Option {
	return func(l *Logger) { l.keepers = append(l.keepers, keepers...) }
}

// WithEnrichers adds enrichers, run in order, after the head decision and before the
// tail keep.
func WithEnrichers(enrichers ...Enricher) Option {
	return func(l *Logger) { l.enrichers = append(l.enrichers, enrichers...) }
}

// headKeep runs the head stage: it asks the configured HeadSampler for the level and
// the trace id, and reports whether the event survives plus the rate that decided it.
//
// An event carrying the reserved audit key bypasses the sampler, because a dropped
// audit line is a hole in a chain (SPEC.md). An absent sampler keeps everything.
func (l *Logger) headKeep(event map[string]any) (keep bool, rate float64) {
	if _, isAudit := event[auditField]; l.headSampler == nil || isAudit {
		return true, 100
	}
	return l.sampleOne(event)
}

// sampleOne runs one HeadSampler under recover. A sampler that panics counts as a keep
// at rate 100, so a broken sampler can never silently drop every event.
func (l *Logger) sampleOne(event map[string]any) (keep bool, rate float64) {
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeHookPanic, sourceName(l.headSampler), fmt.Errorf("panic: %v", r))
			keep, rate = true, 100
		}
	}()
	return l.headSampler.Sample(levelFrom(event), traceIDOf(event))
}

// traceIDOf reads trace.trace_id, the one field of the event the head stage may see
// beside the level.
func traceIDOf(event map[string]any) string {
	trace, _ := event["trace"].(map[string]any)
	id, _ := trace["trace_id"].(string)
	return id
}

// viewOf returns the read-only event a Keeper, a hook, or a summary builder reads.
func viewOf(event map[string]any) eventView {
	kind, _ := event["kind"].(string)
	return eventView{fields: event, kind: kind, level: levelFrom(event)}
}

// keepEvent runs every Keeper and combines the answers with OR: one keeper that says
// yes keeps the event. It returns false when no Keeper is configured, so a head drop
// stands. A keeper that panics counts as a yes, so a broken sampler can never silently
// drop every event.
func (l *Logger) keepEvent(ctx context.Context, event map[string]any) bool {
	for _, k := range l.keepers {
		if l.keepOne(ctx, k, viewOf(event)) {
			return true
		}
	}
	return false
}

// keepOne runs one Keeper under recover.
func (l *Logger) keepOne(ctx context.Context, k Keeper, event Event) (keep bool) {
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeHookPanic, sourceName(k), fmt.Errorf("panic: %v", r))
			keep = true
		}
	}()
	return k.Keep(ctx, event)
}

// startHooks runs every Starter for one unit of work, in registration order. The
// context the last one returns replaces the unit's context. A panic reports
// WLOG_HOOK_PANIC and keeps the context from before that Starter.
func (l *Logger) startHooks(ctx context.Context, kind string) context.Context {
	for _, s := range l.starters {
		ctx = l.safeStart(ctx, s, kind)
	}
	return ctx
}

// safeStart runs one Starter under recover.
func (l *Logger) safeStart(ctx context.Context, s Starter, kind string) (out context.Context) {
	out = ctx
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeHookPanic, sourceName(s), fmt.Errorf("panic: %v", r))
		}
	}()
	return s.OnStart(ctx, kind)
}

// finishHooks runs every Finisher with the read-only event, after finalize and before
// the drains, so a Finisher sees the summary, the outcome, and the redacted fields.
func (l *Logger) finishHooks(ctx context.Context, event map[string]any) {
	if len(l.finishers) == 0 {
		return
	}
	view := viewOf(event)
	for _, f := range l.finishers {
		l.safeFinish(ctx, f, view)
	}
}

// safeFinish runs one Finisher under recover.
func (l *Logger) safeFinish(ctx context.Context, f Finisher, event Event) {
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeHookPanic, sourceName(f), fmt.Errorf("panic: %v", r))
		}
	}()
	f.OnFinish(ctx, event)
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
			l.reportProblem(codeHookPanic, sourceName(en), fmt.Errorf("panic: %v", r))
		}
	}()
	en.Enrich(ctx, event)
}

// finalizeSize runs the last part of the finalize stage: the size cap of SPEC-G7.
// While the event holds more than maxEventSize bytes, it drops the largest user field
// and records the name. The names become wlog.truncated, the drops join
// wlog.dropped_fields, and a report names the code WLOG_EVENT_TOO_LARGE.
//
// It returns the event unchanged when it fits. The order is fixed by size and then by
// name, so two events with the same fields trim the same way (gate G6).
//
// ponytail: user fields only, so a reserved group stays over the cap. Each group is
// bounded by its own field cap, so add reserved keys to the loop if that ceiling matters.
func (l *Logger) finalizeSize(out map[string]any, wlogFields map[string]any) {
	total := valueSize(out)
	if total <= maxEventSize {
		return
	}

	// One size per user key, measured once, so the trim walks the event a fixed
	// number of times however many keys it has.
	keys := make([]string, 0, len(out))
	sizes := make(map[string]int, len(out))
	for key, value := range out {
		if isReservedKey(key) {
			continue
		}
		keys = append(keys, key)
		sizes[key] = len(key) + 8 + valueSize(value)
	}
	sort.Slice(keys, func(i, j int) bool {
		if sizes[keys[i]] != sizes[keys[j]] {
			return sizes[keys[i]] > sizes[keys[j]]
		}
		return keys[i] < keys[j]
	})

	truncated := make([]any, 0, len(keys))
	for _, key := range keys {
		if total <= maxEventSize {
			break
		}
		delete(out, key)
		total -= sizes[key]
		truncated = append(truncated, key)
	}
	if len(truncated) == 0 {
		return
	}
	wlogFields["truncated"] = truncated
	dropped, _ := wlogFields["dropped_fields"].(int)
	wlogFields["dropped_fields"] = dropped + len(truncated)
	l.reportProblem(codeEventTooLarge, "finalize",
		fmt.Errorf("dropped %d fields to fit the %d byte cap", len(truncated), maxEventSize))
}
