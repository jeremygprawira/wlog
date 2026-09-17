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
}

// Caps that bound one event's memory (gate G4). A field beyond its cap is dropped and
// counted in wlog.dropped_fields on the emitted event, rather than growing unbounded.
const (
	maxKeys         = 200       // top-level fields, including group and array field names
	maxAuditRecords = 20        // elements in the reserved audit array (SPEC.md caps)
	auditField      = "audit"   // the one reserved key an event always has room for
	maxGroupFields  = 50        // fields inside one SetGroup group
	maxArrayLen     = 200       // elements in one Append array
	maxEventSize    = 256 << 10 // bytes of field values one event may hold (CORE-25)
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
	if l == nil || !Enabled() {
		return ctx, func() {}
	}
	e := &event{
		fields: map[string]any{}, operation: operation, start: time.Now(),
		extractor: l.errorExtractor, level: LevelInfo,
		strictKeys: l.strictKeysForEvent(), rawValues: l.rawValues,
	}
	ctx = withEvent(ctx, e)
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
// Phase 11 replaces this ceiling with the full size cap of the event shape. Callers must hold
// e.mu.
func (e *event) chargeSize(old, next any) bool {
	growth := valueSize(next) - valueSize(old)
	if e.size+growth > maxEventSize {
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

// emit builds the final event map, redacts it, and writes one JSON line to stdout.
func (l *Logger) emit(e *event) {
	// A closed drain would drop the event silently, so report it instead.
	if l.closed.Load() {
		l.reportError(fmt.Errorf("Logger.Close was called: the event was dropped"), "emit")
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

	// An audit fact is never filtered away: a policy that asks for error-level
	// logs still wants the record of who did what, and a dropped audit line is a
	// hole in a chain that a reader must be able to verify.
	if !audit && levelRank[level] < levelRank[l.minLevel] {
		return
	}

	outcome := "success"
	if level == LevelError {
		outcome = "error"
	}
	out := map[string]any{
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
		"level":       string(level),
		"operation":   e.operation,
		"duration_ms": time.Since(e.start).Milliseconds(),
		"outcome":     outcome,
	}
	if l.service != (serviceInfo{}) {
		out["service"] = map[string]any{
			"name": l.service.name, "version": l.service.version, "env": l.service.env,
		}
	}
	maps.Copy(out, fields)
	if dropped > 0 {
		out["wlog.dropped_fields"] = dropped
	}
	if droppedLogs > 0 {
		out["wlog.dropped_logs"] = droppedLogs
	}
	if lateWrites > 0 {
		out["wlog.late_writes"] = lateWrites
	}
	if len(unknownKeys) > 0 {
		out["wlog.unknown_keys"] = unknownKeys
	}
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

// pipeline runs the fixed per-event stages (SPEC.md): keep/sample, then enrich, then
// redact, then rename, then sinks/drains. A dropped event skips enrich and redact
// entirely; anything an enricher adds still passes through redact, same as any other
// field. Shared by emit (a wide event) and plainLog (a one-off line), so both go
// through exactly the same pipeline.
func (l *Logger) pipeline(ctx context.Context, out map[string]any) {
	if !l.shouldKeep(ctx, out) {
		return
	}
	l.runEnrichers(ctx, out)

	redactor := l.currentRedactor()
	redactor.Apply(out)
	if l.redactFingerprint {
		out["redact.fingerprint"] = redactor.Fingerprint()
	}
	out = applyFieldNames(out, l.fieldNames)
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
	b, err := json.Marshal(out)
	if err != nil {
		l.reportError(err, "stdout")
		return
	}
	_, _ = fmt.Fprintln(os.Stdout, string(b))
}
