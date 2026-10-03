package wlogotel_test

import (
	"fmt"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestOtel_O4_AttributeRankBeforeUserKeys proves reserved attributes survive when
// the span holds more than 128 attributes. Name order would keep the user keys
// and drop http.route, error.type, outcome, and operation.
func TestOtel_O4_AttributeRankBeforeUserKeys(t *testing.T) {
	rec, ctx := recorder(t)
	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "GET /orders")
	wlog.Set(unit, "kind", "request")
	wlog.SetGroup(unit, "http", "method", "GET", "route", "/orders", "status", 500)
	wlog.Error(unit, fmt.Errorf("boom"))
	for i := 0; i < 140; i++ {
		wlog.Set(unit, fmt.Sprintf("a%03d", i), "x")
	}
	end()

	spans := rec.Started()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	attrs := spans[0].Attributes()
	for _, name := range []string{"http.route", "error.type", "outcome", "operation"} {
		if _, ok := findAttr(attrs, name); !ok {
			t.Errorf("span dropped %s among %d attributes", name, len(attrs))
		}
	}
}
