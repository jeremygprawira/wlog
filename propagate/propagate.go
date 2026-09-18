// Package propagate carries the trace context of one unit of work across a process
// boundary: it reads the traceparent, tracestate, and X-Request-ID headers of an
// incoming message, and it writes them onto an outgoing one.
//
// Read top to bottom: carrier.go holds the three header shapes, this file holds Extract,
// Inject, and the trace context, and xray.go holds the read-only B3 and X-Ray fallbacks.
// The package imports no tracing library, so an adapter uses it with any framework.
package propagate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// maxRequestID caps the X-Request-ID header a caller may keep: a longer value is not a
// request id but a fingerprint or a body.
const maxRequestID = 128

// TraceContext is the trace state of one unit of work, as it travels in headers.
type TraceContext struct {
	TraceID      string // 32 lowercase hex
	SpanID       string // 16 lowercase hex, this unit's span
	ParentSpanID string // the caller's span, when one came in
	Sampled      bool
	TraceState   string
	RequestID    string
}

// traceCtxKey is the private key the trace context travels under.
type traceCtxKey struct{}

// config holds the options of one Extract call.
type config struct {
	b3   bool
	xray bool
}

// Option turns on one read-only fallback format.
type Option func(*config)

// WithB3 also reads the B3 single header and the B3 multi headers.
func WithB3() Option { return func(c *config) { c.b3 = true } }

// WithXRay also reads X-Amzn-Trace-Id and AWSTraceHeader.
func WithXRay() Option { return func(c *config) { c.xray = true } }

// Extract reads the trace context of an incoming message, and returns a context that
// carries it.
//
// It follows the W3C Trace Context rules: a version of ff, an all-zero id, and a bad
// length are refused, and a future version with extra fields is accepted. With no valid
// parent it generates a trace id, and it always generates a new span id for this unit.
// WithB3 and WithXRay add the two read-only fallback formats.
//
// The trace group of the event on ctx is replaced, so an adapter that extracts inside a
// unit of work needs no second call.
func Extract(ctx context.Context, c Carrier, opts ...Option) context.Context {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}

	var tc TraceContext
	parent, ok := parseTraceparent(c.Get("traceparent"))
	if ok {
		tc.TraceID, tc.ParentSpanID, tc.Sampled = parent.traceID, parent.spanID, parent.sampled
	}
	if !ok && tc.TraceID == "" && cfg.b3 {
		if trace, span, sampled, found := b3Trace(c); found {
			tc.TraceID, tc.ParentSpanID, tc.Sampled = trace, span, sampled
		}
	}
	if tc.TraceID == "" && cfg.xray {
		if trace, span, sampled, found := xrayTrace(c); found {
			tc.TraceID, tc.ParentSpanID, tc.Sampled = trace, span, sampled
		}
	}
	if tc.TraceID == "" {
		tc.TraceID = randomHex(16)
	}
	tc.SpanID = randomHex(8)
	tc.TraceState = c.Get("tracestate")
	tc.RequestID = requestID(c.Get("X-Request-ID"), tc.TraceID)

	return ContextWith(ctx, tc)
}

// Inject writes the trace context of ctx onto an outgoing message, so the next service
// joins the same trace. Inside a call from wlog.StartCall, the span id is the call's span
// id, so that service links to the call and not to the whole unit of work.
func Inject(ctx context.Context, c Carrier) {
	tc, ok := FromContext(ctx)
	if !ok {
		return
	}
	spanID := tc.SpanID
	if callSpan, ok := wlog.CallSpanID(ctx); ok {
		spanID = callSpan
	}
	c.Set("traceparent", traceparentHeader(tc.TraceID, spanID, tc.Sampled))
	if tc.TraceState != "" {
		c.Set("tracestate", tc.TraceState)
	}
	if tc.RequestID != "" {
		c.Set("X-Request-ID", tc.RequestID)
	}
}

// FromContext returns the trace context of a context, and reports whether it carries one.
func FromContext(ctx context.Context) (TraceContext, bool) {
	tc, ok := ctx.Value(traceCtxKey{}).(TraceContext)
	return tc, ok
}

// ContextWith replaces the trace context of a unit of work, on the context and in the
// trace group of the event on it. An adapter that holds a recording span of its own, such
// as the trace-otel Starter, uses it so the span wins over the ids Extract made.
func ContextWith(ctx context.Context, tc TraceContext) context.Context {
	ctx = context.WithValue(ctx, traceCtxKey{}, tc)
	writeTraceGroup(ctx, tc)
	return ctx
}

// writeTraceGroup records the ids on the event of ctx, when it holds one. A context
// outside a unit of work carries the ids alone.
func writeTraceGroup(ctx context.Context, tc TraceContext) {
	if !wlog.HasEvent(ctx) {
		return
	}
	fields := []any{"trace_id", tc.TraceID, "span_id", tc.SpanID}
	if tc.ParentSpanID != "" {
		fields = append(fields, "parent_span_id", tc.ParentSpanID)
	}
	if tc.RequestID != "" {
		fields = append(fields, "request_id", tc.RequestID)
	}
	wlog.SetGroup(ctx, "trace", fields...)
}

// traceparent is the valid result of one traceparent header.
type traceparent struct {
	traceID string
	spanID  string
	sampled bool
}

// parseTraceparent reads one traceparent header by the rules of W3C Trace Context level
// 1: four fields of version, trace id, parent span id, and flags.
//
// A version of ff is invalid by rule. Version 00 carries exactly four fields, and a later
// version may carry more, which this parser ignores because a reader that does not know
// them reads the four it does.
func parseTraceparent(value string) (traceparent, bool) {
	parts := strings.Split(value, "-")
	if len(parts) < 4 {
		return traceparent{}, false
	}
	if len(parts) > 4 && parts[0] == "00" {
		return traceparent{}, false
	}
	if !isHex(parts[0], 2) || parts[0] == "ff" {
		return traceparent{}, false
	}
	if !isHex(parts[1], 32) || !isHex(parts[2], 16) || !isHex(parts[3], 2) {
		return traceparent{}, false
	}
	if isZero(parts[1]) || isZero(parts[2]) {
		return traceparent{}, false
	}
	flags, err := strconv.ParseUint(parts[3], 16, 8)
	if err != nil {
		return traceparent{}, false
	}
	return traceparent{traceID: parts[1], spanID: parts[2], sampled: flags&1 == 1}, true
}

// traceparentHeader renders one traceparent header, version 00, with the sampled flag in
// the low bit.
func traceparentHeader(traceID, spanID string, sampled bool) string {
	flags := "00"
	if sampled {
		flags = "01"
	}
	return "00-" + traceID + "-" + spanID + "-" + flags
}

// requestID returns the request id of a message: a valid X-Request-ID header, or the
// trace id. A header counts when it holds 128 characters or fewer from [A-Za-z0-9._:-],
// because a longer or stranger value is a body or a fingerprint and not an id.
func requestID(value, traceID string) string {
	if value == "" || len(value) > maxRequestID {
		return traceID
	}
	for i := 0; i < len(value); i++ {
		if !validRequestIDByte(value[i]) {
			return traceID
		}
	}
	return value
}

// validRequestIDByte reports whether b belongs to the request id alphabet.
func validRequestIDByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '_', b == ':', b == '-':
		return true
	}
	return false
}

// isHex reports whether s holds exactly n lowercase hex characters.
func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !validHexByte(s[i]) {
			return false
		}
	}
	return true
}

// validHexByte reports whether b is a lowercase hex digit.
func validHexByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f')
}

// isZero reports whether s holds only zero characters.
func isZero(s string) bool {
	return strings.Trim(s, "0") == ""
}

// randomHex returns n random bytes as lowercase hex, and an empty string when the random
// source fails.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
