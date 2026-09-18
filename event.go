package wlog

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"time"
)

// event holds one unit of work's fields between Start and its end func running. It is
// safe for concurrent Set calls: everything below mu is only ever touched with mu held.
type event struct {
	mu          sync.Mutex
	fields      map[string]any
	operation   string
	start       time.Time
	sealed      bool
	rawValues   bool // skip normalize for a value already in tree form
	dropped     int  // count of Set/SetGroup/Append calls rejected by a cap (G4)
	size        int  // approximate bytes of the stored values (CORE-25)
	droppedLogs int  // count of AppendLog lines rejected by maxLogLines (G4)
	level       Level
	levelSet    bool // true once SetLevel has been called; wins over the default
	extractor   ErrorExtractor
	errInfo     *ErrorInfo      // the error that currently decides the outcome
	errList     []ErrorInfo     // earlier errors, oldest first, capped at maxErrorList
	parent      *event          // set by Detach; nil for a top-level Start event
	lateWrites  int             // writes received after this event sealed (G3, G4)
	strictKeys  map[string]bool // nil unless StrictKeys is active for this event's env
	unknownKeys []string
	ctx         context.Context // the context Start/Detach was given, for the enrich stage
	kind        string          // the event kind: work, log, or a kind SPEC-work sets
	eventID     string          // the UUIDv7 that identifies this event, set at Start
	l           *Logger         // the Logger that built this event, for a late-write report
	caller      bool            // true when wlog.Error records the line that called it
}

// The kind of an event: a unit of work, or a plain log line. SPEC-work adds the
// request, rpc, message, job, command, and function kinds, which its own API sets.
const (
	kindWork = "work"
	kindLog  = "log"
)

// schemaVersion is the version of the event shape this release writes. It travels in
// wlog.schema_version, so a consumer knows which schema to validate against.
const schemaVersion = 2

// Caps that bound one event's memory (gate G4). A field beyond its cap is dropped and
// counted in wlog.dropped_fields on the emitted event, rather than growing unbounded.
const (
	maxKeys         = 200       // top-level fields, including group and array field names
	maxAuditRecords = 20        // elements in the reserved audit array (SPEC.md caps)
	auditField      = "audit"   // the one reserved key an event always has room for
	maxGroupFields  = 50        // fields inside one SetGroup group
	maxArrayLen     = 200       // elements in one Append array
	maxEventSize    = 256 << 10 // the cap finalize enforces on one event (SPEC-G7)
	// maxEventMemory is the ceiling one write stops at, so a single event cannot grow
	// without bound (gate G4). It sits above maxEventSize, because finalize trims the
	// event to the smaller cap and names what it removed in wlog.truncated.
	maxEventMemory = 4 << 20
)

type eventCtxKey struct{}

func withEvent(ctx context.Context, e *event) context.Context {
	return context.WithValue(ctx, eventCtxKey{}, e)
}

func eventFrom(ctx context.Context) *event {
	e, _ := ctx.Value(eventCtxKey{}).(*event)
	return e
}

// HasEvent reports whether ctx carries a wide event that still accepts writes, which
// means one from a Start whose end has not run. Callers that build on top of Start/Set
// (like package audit) use it to tell "inside a request" from "standalone": a sealed
// event counts as standalone, because Set alone cannot tell them apart, it silently
// no-ops either way.
func HasEvent(ctx context.Context) bool {
	e := eventFrom(ctx)
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.sealed
}

// Start begins one wide event. It reads the *Logger attached to ctx (see
// Logger.WithContext); if there is none, Start returns ctx unchanged and a no-op end
// func, so calling it is always safe even before a logger is configured.
//
// Call the returned end func exactly once, typically deferred, to redact and emit the
// event.
func Start(ctx context.Context, operation string) (context.Context, func()) {
	l := loggerFrom(ctx)
	if l == nil {
		return ctx, func() {}
	}
	if !Enabled() {
		l.dropEvent(dropDisabled)
		return ctx, func() {}
	}
	e := &event{
		fields: map[string]any{}, operation: operation, start: time.Now(),
		extractor: l.errorExtractor, level: LevelInfo,
		strictKeys: l.strictKeysForEvent(), rawValues: l.rawValues, l: l, caller: l.errorCaller,
		kind: kindWork, eventID: newEventID(time.Now()),
	}
	// The trace starts here, so a child that Detach makes copies the same trace and
	// an adapter that names the trace later merges into it.
	e.fields["trace"] = map[string]any{"trace_id": newTraceID(), "span_id": newSpanID()}
	ctx = withEvent(ctx, e)
	// Every Starter runs once the event exists, and the context it returns is the
	// one the unit continues with.
	ctx = l.startHooks(ctx, e.kind)
	e.ctx = ctx
	return ctx, func() { l.emit(e) }
}

// Set adds one field to the current event. It is a no-op, never a panic, when ctx
// carries no event (no Start was called) or the event has already been emitted.
//
// Set replaces the value of a key. SetGroup merges, and Append adds to an array,
// so a caller always knows which of the three it called. A struct or any other
// non-JSON-tree value is copied through its json tags, and the copy belongs to
// the event: a later change to the caller's value never reaches a sink.
func Set(ctx context.Context, key string, value any) {
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	// The copy runs before the lock, so a MarshalJSON method that logs through
	// wlog on this same event finishes instead of deadlocking.
	copied := e.copyForStore(value)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}
	if !e.reserveTopLevelSlot(key) {
		return
	}
	if !e.chargeSize(e.fields[key], copied) {
		return
	}
	e.fields[key] = copied
	e.trackUnknownKey(key)
}

// Field returns one top-level field from the current event, and whether that field was
// present. It is a no-op read that returns false when ctx carries no event. The value is
// returned as stored, so a caller must not mutate it. A derived writer, such as llm.Add,
// uses it to fold a running total.
func Field(ctx context.Context, key string) (any, bool) {
	e := eventFrom(ctx)
	if e == nil {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	value, ok := e.fields[key]
	return value, ok
}

// trackUnknownKey records key in unknownKeys (once) if StrictKeys is active for this
// event and key was never registered. Callers must hold e.mu.
func (e *event) trackUnknownKey(key string) {
	if e.strictKeys == nil || e.strictKeys[key] {
		return
	}
	if slices.Contains(e.unknownKeys, key) {
		return
	}
	e.unknownKeys = append(e.unknownKeys, key)
}

// SetGroup merges fields into a named group within the event, creating it on first
// use. Accepts the same shapes as Append/Set: a single map[string]any, or key, value,
// key, value, ... pairs. The group counts as one top-level slot; its own fields are
// capped separately by maxGroupFields.
func SetGroup(ctx context.Context, group string, kv ...any) {
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	pairs := kvToMap(kv)
	if len(pairs) == 0 {
		return
	}
	copied := copyMap(pairs, 1)

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}

	g, ok := e.fields[group].(map[string]any)
	if !ok {
		if !e.reserveTopLevelSlot(group) {
			return
		}
		g = map[string]any{}
		e.fields[group] = g
	}
	for k, v := range copied {
		if _, exists := g[k]; !exists && len(g) >= maxGroupFields {
			e.dropped++
			continue
		}
		if !e.chargeSize(g[k], v) {
			continue
		}
		// A group merges a nested map rather than replacing it, at every depth,
		// so a second call adds to what the first one wrote.
		if existing, ok := g[k].(map[string]any); ok {
			if incoming, ok := v.(map[string]any); ok {
				mergeMap(existing, incoming)
				continue
			}
		}
		g[k] = v
	}
}

// Append appends value to a named array field, creating it on first use. The array
// counts as one top-level slot; its own length is capped separately by maxArrayLen.
func Append(ctx context.Context, key string, value any) {
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	copied := e.copyForStore(value)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}

	arr, ok := e.fields[key].([]any)
	if !ok {
		if !e.reserveTopLevelSlot(key) {
			return
		}
	}
	if len(arr) >= arrayLimit(key) {
		e.dropped++
		return
	}
	grown := append(arr, copied)
	if !e.chargeSize(e.fields[key], grown) {
		return
	}
	e.fields[key] = grown
}

// arrayLimit returns the cap for one array key.
//
// The reserved audit array holds fewer elements than a user array, per the caps in
// SPEC.md. The count is capped for the same reason every other cap exists: one event
// cannot be made to grow without bound (gate G4). A record past the cap counts as a
// dropped field, so it is never lost silently.
func arrayLimit(key string) int {
	if key == auditField {
		return maxAuditRecords
	}
	return maxArrayLen
}

// chargeSize reports whether the event has room for next, where old is the value it
// already counted at that key (nil when the key is new), and counts a write it has to
// drop.
//
// It charges the difference, not the whole value, so the budget tracks what the event holds
// rather than everything ever written to it. A caller that rewrites one growing array, as
// llm.Add does, would otherwise pay for the same entries again on every write and run out of
// room long before the array reached its own cap (gate G4).
//
// The ceiling is maxEventMemory, which only stops unbounded growth. The 256 KiB event cap
// belongs to finalize, which trims the event and names what it removed. Callers must hold
// e.mu.
func (e *event) chargeSize(old, next any) bool {
	growth := valueSize(next) - valueSize(old)
	if e.size+growth > maxEventMemory {
		e.dropped++
		return false
	}
	e.size += growth
	if e.size < 0 {
		e.size = 0
	}
	return true
}

// CountDropped records that an adapter dropped n values because of a cap it applied itself,
// so they appear in wlog.dropped_fields like every other capped write (gate G4). An adapter
// that caps an array inside a group, such as llm.Add, reports through it.
func CountDropped(ctx context.Context, n int) {
	if n <= 0 {
		return
	}
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}
	e.dropped += n
}

// mergeMap copies every field of src into dst, and it merges two maps that share a
// key the same way, so SetGroup reaches the same result at every depth.
func mergeMap(dst, src map[string]any) {
	for key, value := range src {
		if existing, ok := dst[key].(map[string]any); ok {
			if incoming, ok := value.(map[string]any); ok {
				mergeMap(existing, incoming)
				continue
			}
		}
		dst[key] = value
	}
}

// reserveTopLevelSlot reports whether key may occupy a top-level field slot: true if
// it already exists (an update doesn't cost a slot) or there is room under maxKeys.
// Otherwise it counts the rejection and returns false. Callers must hold e.mu.
func (e *event) reserveTopLevelSlot(key string) bool {
	if _, exists := e.fields[key]; exists {
		return true
	}
	if key == auditField {
		// The audit array always has room, even on an event holding every other key:
		// losing an audit record to a key cap would be a hole in the chain (G5).
		return true
	}
	if len(e.fields) >= maxKeys {
		e.dropped++
		return false
	}
	return true
}

// kvToMap parses the flexible SetGroup/Append-style argument list: a single
// map[string]any, or key, value, key, value, ... pairs. A non-string key, or a
// trailing unpaired value, is dropped rather than causing a panic.
func kvToMap(kv []any) map[string]any {
	if len(kv) == 1 {
		if m, ok := kv[0].(map[string]any); ok {
			return m
		}
	}
	if len(kv)%2 != 0 {
		kv = kv[:len(kv)-1]
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			m[k] = kv[i+1]
		}
	}
	return m
}

// copyForStore returns the value an event stores for a caller's value.
//
// Every value is copied into the tree wlog owns, so a caller can never change an
// event after the fact and a drain never sees a map that another goroutine still
// writes. The copy is the same in every mode, because ownership is a gate and
// not a preference. A caller runs this before it takes e.mu.
func (e *event) copyForStore(v any) any {
	return copyValue(v)
}

// normalize passes JSON-tree values through unchanged and round-trips everything else
// (structs, typed slices, ...) through encoding/json so it renders using the value's
// own json tags, the same shape it would have if serialized directly.
func normalize(v any) any {
	switch v.(type) {
	case nil, string, bool, int, int64, float64, map[string]any, []any:
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return fmt.Sprintf("%v", v)
	}
	return out
}

// outcomeOf names the outcome of an event from its level: an event at level error
// failed, and every other event succeeded.
func outcomeOf(level Level) string {
	if level == LevelError {
		return "error"
	}
	return "success"
}

// emit builds the final event map, redacts it, and writes one JSON line to stdout.
func (l *Logger) emit(e *event) {
	// A closed drain would drop the event silently, so report it instead.
	if l.closed.Load() {
		l.reportProblem(codeLoggerClosed, "emit", fmt.Errorf("Logger.Close was called: the event was dropped"))
		l.dropEvent(dropClosed)
		return
	}

	e.mu.Lock()
	fields := make(map[string]any, len(e.fields))
	maps.Copy(fields, e.fields)
	dropped := e.dropped
	droppedLogs := e.droppedLogs
	lateWrites := e.lateWrites
	unknownKeys := e.unknownKeys
	errInfo := e.errInfo
	errList := e.errList
	level := e.level
	audit := e.fields[auditField] != nil
	ctx := e.ctx
	e.sealed = true
	e.mu.Unlock()

	// The measure stage runs before the level filter and before head sampling, so a
	// filter or a sampler never changes a metric.
	l.measureEvent(ctx, e, level)

	// An audit fact is never filtered away: a policy that asks for error-level
	// logs still wants the record of who did what, and a dropped audit line is a
	// hole in a chain that a reader must be able to verify.
	if !audit && levelRank[level] < levelRank[l.minLevel] {
		l.dropEvent(dropLevel)
		return
	}

	outcome := outcomeOf(level)
	kind := e.kind
	if kind == "" {
		kind = kindWork
	}
	// Core names the trace when no enricher and no adapter did, so every event of
	// real work joins the same trace as its children.
	if kind != kindLog {
		if trace, ok := e.fields["trace"].(map[string]any); ok {
			if trace["trace_id"] == nil || trace["trace_id"] == "" {
				trace["trace_id"] = newTraceID()
			}
			if trace["span_id"] == nil || trace["span_id"] == "" {
				trace["span_id"] = newSpanID()
			}
		} else if _, taken := e.fields["trace"]; !taken {
			e.fields["trace"] = map[string]any{"trace_id": newTraceID(), "span_id": newSpanID()}
		}
	}
	out := map[string]any{
		"timestamp": e.start.UTC().Format(time.RFC3339Nano),
		"level":     string(level),
		"operation": e.operation,
		"kind":      kind,
		"outcome":   outcome,
		"event_id":  e.eventID,
	}
	// A log line records no work, so it carries no duration.
	if kind != kindLog {
		out["duration_ms"] = float64(time.Since(e.start).Microseconds()) / 1000
	}
	if l.service != (serviceInfo{}) {
		out["service"] = map[string]any{
			"name": l.service.name, "version": l.service.version, "env": l.service.env,
		}
	}
	maps.Copy(out, fields)
	out["wlog"] = wlogObject("", map[string]any{
		"dropped_fields": dropped,
		"dropped_logs":   droppedLogs,
		"late_writes":    lateWrites,
		"unknown_keys":   unknownKeys,
	})
	// Normalized through normalize() (not assigned directly) so redaction, which only
	// walks map[string]any/[]any/string, sees inside error detail too.
	if errInfo != nil {
		out["error"] = normalize(*errInfo)
	}
	if len(errList) > 0 {
		out["errors"] = normalize(errList)
	}

	l.pipeline(ctx, out)
}

// pipeline runs the fixed per-event stages (SPEC.md): head sample, enrich, tail keep,
// redact, finalize, then the drains and the writers. Head sampling sees the level and
// the trace id only. A Keeper sees the enriched event and can force a head drop back.
// A dropped event skips redact and every later stage.
//
// Shared by emit (a wide event) and plainLog (a one-off line), so both go through
// exactly the same pipeline.
func (l *Logger) pipeline(ctx context.Context, out map[string]any) {
	keep, rate := l.headKeep(out)

	// A head drop that no Keeper can rescue skips every later stage, so the enrich work
	// is not spent on an event nobody reads.
	if !keep && len(l.keepers) == 0 {
		l.dropEvent(dropSampled)
		return
	}
	l.runEnrichers(ctx, out)

	// The tail keep runs when head sampling left room for a rescue: the event was
	// dropped, the sampler kept only a share of events, or no sampler decided at all. An
	// audit event skips the stage, because an audit record is never filtered.
	if _, isAudit := out[auditField]; !isAudit && (!keep || l.headSampler == nil || rate < 100) {
		keep = l.keepEvent(ctx, out) || keep
	}
	if !keep {
		l.dropEvent(dropSampled)
		return
	}

	redactor := l.currentRedactor()
	redactor.Apply(out)
	// The summary is built from the redacted event, so a value the redactor hid
	// cannot reappear inside the sentence that describes the event.
	out["summary"] = l.summarizer(ctx, out, redactor.Replacement())

	wlogFields, _ := out["wlog"].(map[string]any)
	if wlogFields == nil {
		wlogFields = map[string]any{"schema_version": schemaVersion}
		out["wlog"] = wlogFields
	}
	if l.redactFingerprint {
		if print := redactor.Fingerprint(); print != "" {
			wlogFields["redact_fingerprint"] = print
		}
	}
	// A head sampler records the share of events it kept, so a reader can weigh a
	// sampled count. A drop that a Keeper forced back keeps the rate of the drop.
	if l.headSampler != nil {
		wlogFields["sample_rate"] = rate
	}
	out = applyFieldNames(out, l.fieldNames)
	if !l.finalizeSize(out, wlogFields) {
		l.dropEvent(dropTooLarge)
		return
	}

	l.stats.emitted.Add(1)
	l.finishHooks(ctx, out)

	// A drain makes network calls, so it must not inherit the request's cancellation:
	// WithoutCancel keeps the context's values (a span, a tenant) while ignoring a
	// canceled request. Enrichers above keep the live context so they see its values.
	l.sendToDrains(context.WithoutCancel(ctx), out)

	if l.silent {
		return
	}

	if l.resolvedFormat() == FormatPretty {
		writePretty(os.Stdout, out, colorEnabled())
		return
	}
	b, err := encodeEvent(out)
	if err != nil {
		l.reportProblem(codeDrainFailed, "stdout", err)
		return
	}
	_, _ = os.Stdout.Write(b)
}
