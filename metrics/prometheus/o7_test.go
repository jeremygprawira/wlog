package wlogprom_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	wlogprom "github.com/jeremygprawira/wlog/metrics/prometheus"
)

// TestProm_O7_RejectsUnorderedBuckets proves New returns an error when the bucket
// bounds are not strictly increasing, so Measure never panics.
func TestProm_O7_RejectsUnorderedBuckets(t *testing.T) {
	reg := prometheus.NewRegistry()
	if _, err := wlogprom.New(reg, wlogprom.Buckets(1, 0.5)); err == nil {
		t.Fatal("New accepted buckets 1, 0.5")
	}
	if _, err := wlogprom.New(reg, wlogprom.Buckets(1, 1)); err == nil {
		t.Fatal("New accepted equal buckets 1, 1")
	}
}
