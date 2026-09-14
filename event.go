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
}

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
// carries no event (no Start was called) or the event has already been emitted.
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
	e.fields[key] = value
}

// emit builds the final event map, redacts it, and writes one JSON line to stdout.
func (l *Logger) emit(e *event) {
	e.mu.Lock()
	fields := make(map[string]any, len(e.fields))
	maps.Copy(fields, e.fields)
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
