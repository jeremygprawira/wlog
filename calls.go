// This file holds the call records of one unit of work: the calls a request makes to a
// database, a cache, a queue, or another service. An adapter starts a call, and the end
// func appends one record to calls[] and folds the numbers into call_stats.
package wlog

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// maxCalls caps the calls[] array of one event. A record past the cap still counts in
// call_stats and in wlog.dropped_calls, so a reader sees the work that happened (gate G4).
const maxCalls = 50

// Call names one thing a unit of work talks to, so a reader can group and count the calls
// without knowing the library that made them.
type Call struct {
	Kind      string // http, db, cache, queue, rpc, llm, storage, or other
	System    string // postgresql, redis, kafka, grpc, aws.s3, anthropic, and so on
	Operation string // GET, SELECT, publish, or messages.create
	Target    string // host and route, table, topic, bucket, or model
}

// CallResult reports how one call ended. Err counts as an error and its text is never
// copied into the record, because a driver error can hold a query, a value, or a
// credential. An adapter that knows a message is safe passes it in ErrMessage.
type CallResult struct {
	Status     string         // 200, OK, NOT_FOUND, and so on
	Err        error          // counts as an error, never copied as text
	ErrCode    string         // SQLSTATE, AWS error code, Redis prefix
	ErrMessage string         // only a message the adapter knows is safe
	Rows       int64          // rows or items affected, 0 means unknown
	Attrs      map[string]any // fields of this call, copied and redacted like any other
	// Duration is the duration the driver measured, for a driver that reports one.
	// Zero means the measured duration of the call, from StartCall to its end func.
	Duration time.Duration
}

// openCall is the state one started call carries on the context, so an adapter inside
// another adapter can see the call it sits in.
type openCall struct {
	call   Call
	spanID string
	start  time.Time
}

type callCtxKey struct{}

// StartCall records the start of one outgoing call, and returns the context of the call
// plus the end func that records it.
//
// The context carries the call, so CallFromContext and CallSpanID read it. A call that
// starts inside a call of the same kind records nothing, because an adapter over a wrapped
// driver would otherwise count one call twice. The end func records once, under recover,
// and it never changes the caller's error.
//
// With no event on ctx, StartCall returns ctx and an end func that does nothing, and it
// reports nothing, because a call outside a unit of work is not a fault.
func StartCall(ctx context.Context, c Call) (context.Context, func(CallResult)) {
	e := eventFrom(ctx)
	if e == nil {
		return ctx, func(CallResult) {}
	}
	// A nested call of the same kind belongs to the call outside it.
	if outer, ok := CallFromContext(ctx); ok && outer.Kind == c.Kind {
		return ctx, func(CallResult) {}
	}

	call := openCall{call: c, spanID: newSpanID(), start: time.Now()}
	ctx = context.WithValue(ctx, callCtxKey{}, call)

	var once sync.Once
	end := func(result CallResult) {
		once.Do(func() { e.safeRecordCall(call, result) })
	}
	return ctx, end
}

// CallFromContext returns the call the context is inside, so an adapter under another
// adapter can skip a record the outer one already makes.
func CallFromContext(ctx context.Context) (Call, bool) {
	call, ok := ctx.Value(callCtxKey{}).(openCall)
	if !ok {
		return Call{}, false
	}
	return call.call, true
}

// CallSpanID returns the span id of the call the context is inside, so a propagator can
// inject the headers of that call.
func CallSpanID(ctx context.Context) (string, bool) {
	call, ok := ctx.Value(callCtxKey{}).(openCall)
	if !ok || call.spanID == "" {
		return "", false
	}
	return call.spanID, true
}

// safeRecordCall records one call under recover, so an end func never panics into the
// caller.
func (e *event) safeRecordCall(call openCall, result CallResult) {
	defer func() {
		if r := recover(); r != nil {
			e.reportHookPanic("call", r)
		}
	}()
	e.recordCall(call, result)
}

// reportHookPanic reports a panic from a call hook through the Logger of this event.
func (e *event) reportHookPanic(source string, recovered any) {
	if e.l != nil {
		e.l.reportProblem(codeHookPanic, source, fmt.Errorf("panic: %v", recovered))
	}
}

// recordCall appends one record to calls[] and folds the numbers into call_stats.
//
// A call that ends after the event emitted records nothing but a late write. The array
// stops at maxCalls records, and every call still counts in call_stats.
func (e *event) recordCall(call openCall, result CallResult) {
	duration := float64(time.Since(call.start).Microseconds()) / 1000
	if result.Duration > 0 {
		duration = float64(result.Duration.Microseconds()) / 1000
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}

	record := map[string]any{
		"kind":        call.call.Kind,
		"operation":   call.call.Operation,
		"duration_ms": duration,
		"span_id":     call.spanID,
	}
	if call.call.System != "" {
		record["system"] = call.call.System
	}
	if call.call.Target != "" {
		record["target"] = call.call.Target
	}
	if result.Status != "" {
		record["status"] = result.Status
	}
	if result.Rows > 0 {
		record["rows"] = result.Rows
	}
	if len(result.Attrs) > 0 {
		record["attrs"] = copyMap(result.Attrs, 1)
	}
	if detail := e.callError(result); detail != nil {
		record["error"] = detail
	}

	calls, _ := e.fields["calls"].([]any)
	if len(calls) < maxCalls {
		if !e.reserveTopLevelSlot("calls") {
			return
		}
		e.fields["calls"] = append(calls, record)
	} else {
		e.droppedCalls++
	}
	e.foldCallStats(call.call.Kind, duration, result.Err != nil)
}

// callError builds the error object of one call record: a code, from ErrCode or from the
// extractor of this event, and only the message the adapter marked safe. It returns nil
// for a call that failed in no way.
func (e *event) callError(result CallResult) map[string]any {
	if result.Err == nil && result.ErrCode == "" {
		return nil
	}
	code := result.ErrCode
	if code == "" && result.Err != nil {
		code = safeExtract(e.l, e.extractor, result.Err).Code
	}
	detail := map[string]any{}
	if code != "" {
		detail["code"] = code
	}
	if result.ErrMessage != "" {
		detail["message"] = result.ErrMessage
	}
	if len(detail) == 0 {
		return nil
	}
	return detail
}

// foldCallStats adds one call to the totals of its kind: the count, the errors, the total
// duration, and the longest one. Callers must hold e.mu.
func (e *event) foldCallStats(kind string, durationMS float64, failed bool) {
	stats, _ := e.fields["call_stats"].(map[string]any)
	if stats == nil {
		if !e.reserveTopLevelSlot("call_stats") {
			return
		}
		stats = map[string]any{}
		e.fields["call_stats"] = stats
	}
	entry, _ := stats[kind].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		stats[kind] = entry
	}
	entry["count"] = numberInt(entry["count"]) + 1
	total := numberFloat(entry["duration_ms"]) + durationMS
	entry["duration_ms"] = total
	if max := numberFloat(entry["max_ms"]); durationMS > max {
		entry["max_ms"] = durationMS
	}
	if failed {
		entry["errors"] = numberInt(entry["errors"]) + 1
	}
}

// numberInt reads a stored counter, treating a missing value as 0.
func numberInt(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	default:
		return 0
	}
}

// numberFloat reads a stored duration, treating a missing value as 0.
func numberFloat(value any) float64 {
	switch number := value.(type) {
	case int:
		return float64(number)
	case int64:
		return float64(number)
	case float64:
		return number
	default:
		return 0
	}
}
