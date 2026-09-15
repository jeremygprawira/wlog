package wlog

import (
	"context"
	"maps"
	"time"
)

// Detach starts a new, independent event for work that outlives the current one (a
// goroutine still running after an HTTP response is sent, a fire-and-forget email
// send). The child copies the parent's trace group and records
// trace.parent_operation, then is emitted on its own end func — it does not wait for,
// or get merged into, the parent.
func Detach(ctx context.Context, operation string) (context.Context, func()) {
	l := loggerFrom(ctx)
	if l == nil {
		return ctx, func() {}
	}
	e := &event{
		fields: map[string]any{}, operation: operation, start: time.Now(),
		extractor: l.errorExtractor, level: LevelInfo,
	}

	if parent := eventFrom(ctx); parent != nil {
		e.parent = parent
		parent.mu.Lock()
		trace := map[string]any{}
		if pt, ok := parent.fields["trace"].(map[string]any); ok {
			maps.Copy(trace, pt)
		}
		parentOp := parent.operation
		parent.mu.Unlock()
		trace["parent_operation"] = parentOp
		e.fields["trace"] = trace
	}

	ctx = withEvent(ctx, e)
	e.ctx = ctx
	return ctx, func() { l.emit(e) }
}

// recordLateWrite is called (with e.mu already held by the caller) when a write lands
// on an event that has already been emitted. It walks up to the nearest ancestor that
// is still open and counts the write there as wlog.late_writes; if every ancestor is
// already sealed too (or there is none), the write is dropped silently rather than
// lost track of forever or, worse, causing a panic (gate G3).
func (e *event) recordLateWrite() {
	for p := e.parent; p != nil; {
		p.mu.Lock()
		if !p.sealed {
			p.lateWrites++
			p.mu.Unlock()
			return
		}
		next := p.parent
		p.mu.Unlock()
		p = next
	}
}
