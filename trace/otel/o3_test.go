package wlogotel_test

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestOtel_O3_SkipsLogAndNestedUnit proves a log line and a failed inner unit do
// not write onto the span a request already claimed.
func TestOtel_O3_SkipsLogAndNestedUnit(t *testing.T) {
	rec, ctx := recorder(t)
	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))

	outer, end := wlog.Start(log.WithContext(ctx), "GET /orders")
	wlog.Set(outer, "kind", "request")
	wlog.SetGroup(outer, "http", "route", "/orders", "status", 200)

	inner, endInner := wlog.Detach(outer, "job nightly")
	wlog.Set(inner, "kind", "job")
	wlog.Error(inner, errors.New("inner boom"))
	endInner()

	wlog.Info(outer, "hello", "note", "from-log")
	end()

	spans := rec.Started()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Status().Code == codes.Error {
		t.Errorf("span status = Error, want the inner unit to leave the request span")
	}
	if _, ok := findAttr(span.Attributes(), "note"); ok {
		t.Error("a log line wrote note onto the request span")
	}
	if _, ok := findAttr(span.Attributes(), "http.route"); !ok {
		t.Error("the request span lost http.route")
	}
}
