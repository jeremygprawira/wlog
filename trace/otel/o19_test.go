package wlogotel_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestOtel_O19_Request5xxErrorType proves a 500 with no error still sets error.type.
func TestOtel_O19_Request5xxErrorType(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithSpans(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(context.Background()), "GET /orders")
	wlog.Set(unit, "kind", "request")
	wlog.SetGroup(unit, "http", "method", "GET", "status", 500)
	end()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	point := firstHistogram(t, rm, "http.server.request.duration")
	got, ok := point.Attributes.Value(attribute.Key("error.type"))
	if !ok || got.AsString() != "500" {
		t.Fatalf("error.type = %v present=%v, want 500", point.Attributes.ToSlice(), ok)
	}
}
