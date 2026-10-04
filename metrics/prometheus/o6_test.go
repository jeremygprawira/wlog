package wlogprom_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogprom "github.com/jeremygprawira/wlog/metrics/prometheus"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/prometheus/client_golang/prometheus"
)

// TestProm_O6_ReusesHistogram proves a second New records on the histogram already registered.
func TestProm_O6_ReusesHistogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	if _, err := wlogprom.New(reg); err != nil {
		t.Fatalf("first New: %v", err)
	}
	second, err := wlogprom.New(reg)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	second.Measure(context.Background(), wlog.Measure{Kind: "job", Operation: "from-second", Outcome: "success", DurationMS: 10})

	if !labelValue(t, reg, "operation", "from-second") {
		t.Error("the second recorder did not write the registered histogram")
	}
}

// TestProm_O6_OtherOperation proves a kind past its cap records _OTHER, not the raw name.
func TestProm_O6_OtherOperation(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := wlogprom.New(reg, wlogprom.MaxOperations(1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	rec.Measure(ctx, wlog.Measure{Kind: "job", Operation: "first"})
	rec.Measure(ctx, wlog.Measure{Kind: "job", Operation: "second"})

	if !labelValue(t, reg, "operation", "_OTHER") {
		t.Error("the capped operation was not recorded as _OTHER")
	}
	if labelValue(t, reg, "operation", "second") {
		t.Error("the capped operation kept the raw name second")
	}
}

// TestProm_O6_DroppedCounter proves the collector exports a dropped event, not only emitted.
func TestProm_O6_DroppedCounter(t *testing.T) {
	log, _ := wlogtest.New(t, wlog.WithLevel(wlog.LevelError))
	_, end := wlog.Start(log.WithContext(context.Background()), "op")
	end()

	reg := prometheus.NewRegistry()
	reg.MustRegister(wlogprom.StatsCollector(log))
	if !labelValue(t, reg, "reason", "level") {
		t.Error("the collector did not export the level drop")
	}
}

func labelValue(t *testing.T, reg *prometheus.Registry, name, want string) bool {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == name && label.GetValue() == want {
					return true
				}
			}
		}
	}
	return false
}
