package wlogprom_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremygprawira/wlog"
	wlogprom "github.com/jeremygprawira/wlog/metrics/prometheus"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// reportDrain reports drain numbers, so a collector that still exports them shows a point.
type reportDrain struct{}

func (reportDrain) Send(context.Context, map[string]any) {}

func (reportDrain) Stats() wlog.DrainStats {
	return wlog.DrainStats{Name: "pipe", Queued: 4, Sent: 3, Dropped: 1, Retried: 2}
}

// TestProm_O5_NoDrainCounter proves a scrape does not export drain counters.
func TestProm_O5_NoDrainCounter(t *testing.T) {
	log, _ := wlogtest.New(t, wlog.WithDrains(reportDrain{}))
	reg := prometheus.NewRegistry()
	reg.MustRegister(wlogprom.StatsCollector(log))

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == "wlog_drain_events_total" {
			t.Fatalf("drain counter exported: %v", family)
		}
	}
}
