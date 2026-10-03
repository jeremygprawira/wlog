package propagate_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestPropagate_CI_CarrierTraceReplacesStarter proves a traceparent on the carrier
// replaces ids a Starter already wrote.
func TestPropagate_CI_CarrierTraceReplacesStarter(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx = propagate.ContextWith(ctx, propagate.TraceContext{
		TraceID: "0123456789abcdef0123456789abcdef",
		SpanID:  "fedcba9876543210",
		Sampled: true,
	})
	ctx = propagate.Extract(ctx, propagate.MapCarrier{
		"traceparent": "00-105445aa7843bc8bf206b12000100000-0000000000000001-01",
	})
	end()

	got, ok := propagate.FromContext(ctx)
	if !ok || got.TraceID != "105445aa7843bc8bf206b12000100000" || got.ParentSpanID != "0000000000000001" || !got.Sampled {
		t.Fatalf("context trace = %+v present=%v, want the carrier trace", got, ok)
	}
	trace, _ := rec.Last()["trace"].(map[string]any)
	if trace["trace_id"] != "105445aa7843bc8bf206b12000100000" || trace["parent_span_id"] != "0000000000000001" {
		t.Fatalf("event trace = %v, want the carrier trace", trace)
	}
}
