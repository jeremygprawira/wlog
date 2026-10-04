package wlogotel_test

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// reportDrain reports drain numbers, so a meter that still exports them shows a point.
type reportDrain struct{}

func (reportDrain) Send(context.Context, map[string]any) {}

func (reportDrain) Stats() wlog.DrainStats {
	return wlog.DrainStats{Name: "pipe", Queued: 4, Sent: 3, Dropped: 1, Retried: 2}
}

// TestOtel_O5_NoDrainCounter proves a scrape does not export wlog.drain.events.
func TestOtel_O5_NoDrainCounter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithSpans(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p), wlog.WithDrains(reportDrain{}))
	_, end := wlog.Start(log.WithContext(context.Background()), "op")
	end()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "wlog.drain.events" {
				t.Fatalf("drain counter exported: %+v", m)
			}
		}
	}
}
