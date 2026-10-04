package work_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

const (
	o2TraceID = "0123456789abcdef0123456789abcdef"
	o2SpanID  = "fedcba9876543210"
)

type starterIDs struct{}

func (starterIDs) Name() string { return "starter-ids" }

func (starterIDs) OnStart(ctx context.Context, _ string) context.Context {
	return propagate.ContextWith(ctx, propagate.TraceContext{
		TraceID: o2TraceID,
		SpanID:  o2SpanID,
		Sampled: true,
	})
}

// TestWork_O2_KeepsStarterSpanID proves a carrier with no traceparent does not
// replace the trace id, span id, and sampled flag a Starter wrote.
func TestWork_O2_KeepsStarterSpanID(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(starterIDs{}))
	var got propagate.TraceContext
	var ok bool
	err := work.Run(context.Background(), log, work.Unit{
		Kind:    work.KindJob,
		Carrier: propagate.NewBytesCarrier(map[string][]byte{}),
	}, func(ctx context.Context) error {
		got, ok = propagate.FromContext(ctx)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ok || got.TraceID != o2TraceID || got.SpanID != o2SpanID || !got.Sampled {
		t.Errorf("context trace = %+v present=%v, want the starter ids", got, ok)
	}
	trace, _ := rec.Last()["trace"].(map[string]any)
	if trace["trace_id"] != o2TraceID || trace["span_id"] != o2SpanID {
		t.Errorf("event trace = %v, want the starter ids", trace)
	}
}
