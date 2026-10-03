package wlogprom_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	wlogprom "github.com/jeremygprawira/wlog/metrics/prometheus"
)

// TestProm_O12_NilDoesNotPanic proves New(nil) returns an error and a nil logger
// does not panic a scrape.
func TestProm_O12_NilDoesNotPanic(t *testing.T) {
	if _, err := wlogprom.New(nil); err == nil {
		t.Fatal("New(nil) returned no error")
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(wlogprom.StatsCollector(nil))
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("Gather: %v", err)
	}
}
