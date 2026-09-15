package wlogotel_test

import (
	"context"
	"testing"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// testSpanContext builds a valid span context directly, so no real tracer or exporter
// is needed to test the enricher.
func testSpanContext(t *testing.T) oteltrace.SpanContext {
	t.Helper()
	traceID, err := oteltrace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("bad trace id: %v", err)
	}
	spanID, err := oteltrace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("bad span id: %v", err)
	}
	return oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: oteltrace.FlagsSampled,
	})
}

func traceGroup(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	group, ok := event["trace"].(map[string]any)
	if !ok {
		t.Fatalf("event has no trace group: %v", event["trace"])
	}
	return group
}

// TestOtel_ValidSpanOverridesTraceparent proves a valid active span wins over values
// http-std derived from a traceparent header.
func TestOtel_ValidSpanOverridesTraceparent(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithEnrichers(wlogotel.Enricher()))
	base := oteltrace.ContextWithSpanContext(context.Background(), testSpanContext(t))

	ctx := log.WithContext(base)
	ctx, end := wlog.Start(ctx, "op")
	wlog.SetGroup(ctx, "trace", "trace_id", "header-trace", "span_id", "header-span")
	end()

	group := traceGroup(t, rec.Last())
	if group["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace_id = %v, want the span's own id", group["trace_id"])
	}
	if group["span_id"] != "00f067aa0ba902b7" {
		t.Errorf("span_id = %v, want the span's own id", group["span_id"])
	}
}

// TestOtel_NoSpanKeepsTraceparent proves the enricher never writes an empty or invalid
// value over the traceparent-derived ids http-std already set.
func TestOtel_NoSpanKeepsTraceparent(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithEnrichers(wlogotel.Enricher()))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	wlog.SetGroup(ctx, "trace", "trace_id", "header-trace", "span_id", "header-span")
	end()

	group := traceGroup(t, rec.Last())
	if group["trace_id"] != "header-trace" || group["span_id"] != "header-span" {
		t.Errorf("trace group = %v, want the header values untouched", group)
	}
}

// TestOtel_InvalidSpanContextIgnored proves a zero-value span context is treated as no
// span, using the OTel API's own IsValid check.
func TestOtel_InvalidSpanContextIgnored(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithEnrichers(wlogotel.Enricher()))
	base := oteltrace.ContextWithSpanContext(context.Background(), oteltrace.SpanContext{})

	ctx := log.WithContext(base)
	ctx, end := wlog.Start(ctx, "op")
	wlog.SetGroup(ctx, "trace", "trace_id", "header-trace")
	end()

	group := traceGroup(t, rec.Last())
	if group["trace_id"] != "header-trace" {
		t.Errorf("trace_id = %v, want the header value untouched", group["trace_id"])
	}
}
