// Package wlogotel fills an event's trace ids from an active OpenTelemetry span. It
// serves a team that already runs OTel tracing, without requiring OTel for everyone
// else: http-std's own traceparent parsing covers the common case with no dependency.
package wlogotel

import (
	"context"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
)

// Enricher returns a wlog.Enricher that reads the active span from the event's context
// and writes trace.trace_id and trace.span_id from it. When the span context is not
// valid (no span, or a zero one) it writes nothing, so traceparent-derived values set
// earlier by http-std pass through untouched.
func Enricher() wlog.Enricher {
	return wlog.EnricherFunc(func(ctx context.Context, event map[string]any) {
		span := oteltrace.SpanContextFromContext(ctx)
		if !span.IsValid() {
			return
		}
		group, ok := event["trace"].(map[string]any)
		if !ok {
			group = map[string]any{}
			event["trace"] = group
		}
		group["trace_id"] = span.TraceID().String()
		group["span_id"] = span.SpanID().String()
	})
}
