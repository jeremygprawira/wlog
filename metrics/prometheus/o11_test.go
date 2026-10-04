package wlogprom_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremygprawira/wlog"
	wlogprom "github.com/jeremygprawira/wlog/metrics/prometheus"
)

// TestProm_O11_ReuseSharesBucketsAndCap proves a second New does not ignore its
// buckets, and two recorders share one operation cap.
func TestProm_O11_ReuseSharesBucketsAndCap(t *testing.T) {
	reg := prometheus.NewRegistry()
	if _, err := wlogprom.New(reg, wlogprom.Buckets(1, 2), wlogprom.MaxOperations(1)); err != nil {
		t.Fatalf("first New: %v", err)
	}
	if _, err := wlogprom.New(reg, wlogprom.Buckets(1, 3), wlogprom.MaxOperations(1)); err == nil {
		t.Fatal("second New ignored different buckets")
	}

	reg = prometheus.NewRegistry()
	first, err := wlogprom.New(reg, wlogprom.MaxOperations(1))
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	second, err := wlogprom.New(reg, wlogprom.MaxOperations(1))
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	ctx := context.Background()
	first.Measure(ctx, wlog.Measure{Kind: "job", Operation: "first"})
	second.Measure(ctx, wlog.Measure{Kind: "job", Operation: "second"})
	if labelValue(t, reg, "operation", "second") {
		t.Error("the second recorder kept its own cap and recorded second")
	}
	if !labelValue(t, reg, "operation", "_OTHER") {
		t.Error("the shared cap did not record _OTHER")
	}
}
