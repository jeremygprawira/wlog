package wlogotel_test

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestOtel_O9_SpansOffKeepsTraceID proves WithSpans(false) still copies the span ids.
func TestOtel_O9_SpansOffKeepsTraceID(t *testing.T) {
	_, ctx := recorder(t)
	sc := oteltrace.SpanContextFromContext(ctx)
	p, err := wlogotel.Plugin(wlogotel.WithSpans(false), wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, events := wlogtest.New(t, wlog.WithPlugins(p))
	_, end := wlog.Start(log.WithContext(ctx), "op")
	end()

	group, _ := events.Last()["trace"].(map[string]any)
	if group["trace_id"] != sc.TraceID().String() || group["span_id"] != sc.SpanID().String() {
		t.Errorf("trace = %v, want the span ids", group)
	}
}

// TestOtel_O9_StatsWithoutMetrics proves WithStats exports counters when metrics are off.
func TestOtel_O9_StatsWithoutMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithMetrics(false), wlogotel.WithSpans(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	_, end := wlog.Start(log.WithContext(context.Background()), "op")
	end()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumOf(t, rm, "wlog.events.emitted"); got != 1 {
		t.Errorf("wlog.events.emitted = %d, want 1", got)
	}
}
