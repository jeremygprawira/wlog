// This file tests the Prometheus recorder with a golden exposition. It uses
// GatherAndCompare rather than CollectAndCompare, because the pinned client_golang
// v1.11.1 registry is a Gatherer and not yet a Collector.
package wlogprom_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jeremygprawira/wlog"
	wlogprom "github.com/jeremygprawira/wlog/metrics/prometheus"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestProm_Histogram proves three events record one histogram that matches the golden
// exposition, with a kind, an operation, an outcome, and a status label.
func TestProm_Histogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	rec, err := wlogprom.New(reg, wlogprom.Buckets(0.5))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	rec.Measure(ctx, wlog.Measure{Kind: "job", Operation: "cleanup", Outcome: "success", DurationMS: 1000})
	rec.Measure(ctx, wlog.Measure{Kind: "job", Operation: "import", Outcome: "error", DurationMS: 2000})
	rec.Measure(ctx, wlog.Measure{Kind: "request", Operation: "GET /orders", Outcome: "error", Status: "502", DurationMS: 250})

	golden, err := os.Open("testdata/histogram.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = golden.Close() }()
	if err := testutil.GatherAndCompare(reg, golden, "wlog_duration_seconds"); err != nil {
		t.Error(err)
	}
}

// TestProm_RegisterTwice proves a second New on one registry reuses the histogram.
func TestProm_RegisterTwice(t *testing.T) {
	reg := prometheus.NewRegistry()
	if _, err := wlogprom.New(reg); err != nil {
		t.Fatalf("first New: %v", err)
	}
	if _, err := wlogprom.New(reg); err != nil {
		t.Fatalf("second New: %v", err)
	}
}

// TestProm_OperationCap proves a kind past its cap records as _OTHER and reports
// WLOG_CAP_REACHED once.
func TestProm_OperationCap(t *testing.T) {
	var mu sync.Mutex
	var problems []wlog.Problem
	reg := prometheus.NewRegistry()
	rec, err := wlogprom.New(reg, wlogprom.MaxOperations(2))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wlogtest.New(t, wlog.WithPlugins(rec), wlog.OnProblem(func(p wlog.Problem) {
		mu.Lock()
		problems = append(problems, p)
		mu.Unlock()
	}))

	ctx := context.Background()
	for _, operation := range []string{"a", "b", "c", "d"} {
		rec.Measure(ctx, wlog.Measure{Kind: "job", Operation: operation})
	}

	mu.Lock()
	defer mu.Unlock()
	caps := 0
	for _, p := range problems {
		if p.Code == "WLOG_CAP_REACHED" {
			caps++
		}
	}
	if caps != 1 {
		t.Errorf("WLOG_CAP_REACHED reported %d times, want 1", caps)
	}
}

// TestProm_StatsCollector proves the collector exports the Logger's counters on a scrape.
func TestProm_StatsCollector(t *testing.T) {
	log, _ := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	end()

	reg := prometheus.NewRegistry()
	reg.MustRegister(wlogprom.StatsCollector(log))

	golden := strings.NewReader(`# HELP wlog_events_emitted_total Events that reached the drains and the writers.
# TYPE wlog_events_emitted_total counter
wlog_events_emitted_total 1
`)
	if err := testutil.GatherAndCompare(reg, golden, "wlog_events_emitted_total"); err != nil {
		t.Error(err)
	}
}
