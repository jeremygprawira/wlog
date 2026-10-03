package httpcore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

const (
	o2TraceID = "0123456789abcdef0123456789abcdef"
	o2SpanID  = "fedcba9876543210"
)

// starterIDs copies fixed trace ids onto the event, the way the OTel Starter does.
type starterIDs struct{}

func (starterIDs) Name() string { return "starter-ids" }

func (starterIDs) OnStart(ctx context.Context, _ string) context.Context {
	return propagate.ContextWith(ctx, propagate.TraceContext{
		TraceID: o2TraceID,
		SpanID:  o2SpanID,
		Sampled: true,
	})
}

// TestHTTPCore_O2_KeepsStarterSpanID proves Extract does not replace the trace id,
// span id, and sampled flag a Starter wrote when the request has no traceparent.
func TestHTTPCore_O2_KeepsStarterSpanID(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(starterIDs{}))
	var got propagate.TraceContext
	var ok bool
	handler := httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = propagate.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if !ok || got.TraceID != o2TraceID || got.SpanID != o2SpanID || !got.Sampled {
		t.Errorf("context trace = %+v present=%v, want the starter ids", got, ok)
	}
	trace, _ := rec.Last()["trace"].(map[string]any)
	if trace["trace_id"] != o2TraceID || trace["span_id"] != o2SpanID {
		t.Errorf("event trace = %v, want the starter ids", trace)
	}
}
