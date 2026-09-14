package wlog

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/jeremygprawira/wlog/redact"
)

// event holds one unit of work's fields between Start and its end func running. It is
// safe for concurrent Set calls: everything below mu is only ever touched with mu held.
type event struct {
	mu        sync.Mutex
	fields    map[string]any
	operation string
	start     time.Time
	sealed    bool
	dropped   int // count of Set/SetGroup/Append calls rejected by a cap (G4)
}

// Caps that bound one event's memory (gate G4). A field beyond its cap is dropped and
// counted in wlog.dropped_fields on the emitted event, rather than growing unbounded.
const (
	maxKeys        = 200 // top-level fields, including group and array field names
	maxGroupFields = 50  // fields inside one SetGroup group
	maxArrayLen    = 200 // elements in one Append array
)

type eventCtxKey struct{}

func withEvent(ctx context.Context, e *event) context.Context {
	return context.WithValue(ctx, eventCtxKey{}, e)
}

func eventFrom(ctx context.Context) *event {
	e, _ := ctx.Value(eventCtxKey{}).(*event)
	return e
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
	e := &event{fields: map[string]any{}, operation: operation, start: time.Now()}
	return withEvent(ctx, e), func() { l.emit(e) }
}

// Set adds one field to the current event. It is a no-op, never a panic, when ctx
// carries no event (no Start was called) or the event has already been emitted. A
// struct or other non-JSON-tree value is normalized via its json tags, same as if it
// had gone through json.Marshal/Unmarshal.
func Set(ctx context.Context, key string, value any) {
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		return
	}
	if !e.reserveTopLevelSlot(key) {
		return
	}
	e.fields[key] = normalize(value)
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

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
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
	for k, v := range pairs {
		if _, exists := g[k]; !exists && len(g) >= maxGroupFields {
			e.dropped++
			continue
		}
		g[k] = normalize(v)
	}
}

// Append appends value to a named array field, creating it on first use. The array
// counts as one top-level slot; its own length is capped separately by maxArrayLen.
func Append(ctx context.Context, key string, value any) {
	e := eventFrom(ctx)
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		return
	}

	arr, ok := e.fields[key].([]any)
	if !ok {
		if !e.reserveTopLevelSlot(key) {
			return
		}
	}
	if len(arr) >= maxArrayLen {
		e.dropped++
		return
	}
	e.fields[key] = append(arr, normalize(value))
}

// reserveTopLevelSlot reports whether key may occupy a top-level field slot: true if
// it already exists (an update doesn't cost a slot) or there is room under maxKeys.
// Otherwise it counts the rejection and returns false. Callers must hold e.mu.
func (e *event) reserveTopLevelSlot(key string) bool {
	if _, exists := e.fields[key]; exists {
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
	e.mu.Lock()
	fields := make(map[string]any, len(e.fields))
	maps.Copy(fields, e.fields)
	dropped := e.dropped
	e.sealed = true
	e.mu.Unlock()

	out := map[string]any{
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
		"level":       "info",
		"operation":   e.operation,
		"duration_ms": time.Since(e.start).Milliseconds(),
		"outcome":     "success",
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

	redactor := l.redactor
	if redactor == nil {
		redactor = redact.Default()
	}
	redactor.Apply(out)

	b, err := json.Marshal(out)
	if err != nil {
		return // never panics; a later task adds an OnError hook for this
	}
	fmt.Fprintln(os.Stdout, string(b))
}
