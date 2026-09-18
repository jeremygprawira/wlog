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
	if l == nil || !Enabled() {
		return ctx, func() {}
	}
	e := &event{
		fields: map[string]any{}, operation: operation, start: time.Now(),
		extractor: l.errorExtractor, level: LevelInfo, rawValues: l.rawValues,
		strictKeys: l.strictKeysForEvent(), l: l, caller: l.errorCaller,
		kind: kindWork, eventID: newEventID(time.Now()),
	}

	if parent := eventFrom(ctx); parent != nil {
		e.l = l
		e.parent = parent
		parent.mu.Lock()
		trace := map[string]any{"trace_id": newTraceID()}
		if pt, ok := parent.fields["trace"].(map[string]any); ok {
			maps.Copy(trace, pt)
			// The child joins the parent's trace. It names its own span and points
			// at the parent's, so a reader walks the trace in either direction.
			trace["parent_span_id"] = pt["span_id"]
			trace["parent_event_id"] = parent.eventID
		}
		trace["span_id"] = newSpanID()
		parentOp := parent.operation
		parent.mu.Unlock()
		trace["parent_operation"] = parentOp
		e.fields["trace"] = trace
	}

	// The work outlives the request, so the child context must not die with the
	// parent. WithoutCancel keeps every value of the parent, including the
	// logger, and drops only the cancellation.
	bg := context.WithoutCancel(ctx)
	ctx = withEvent(bg, e)
	e.ctx = ctx
	return ctx, func() { l.emit(e) }
}

// recordLateWrite is called (with e.mu already held by the caller) when a write lands
// on an event that has already been emitted. It walks up to the nearest ancestor that
// is still open and counts the write there as wlog.late_writes; if every ancestor is
// already sealed too (or there is none), the write is reported as WLOG_LATE_WRITE
// rather than lost forever or, worse, causing a panic (gate G3).
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
	if e.l != nil {
		e.l.reportProblem(codeLateWrite, "write", nil)
	}
}
