package wlogotel_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestOtel_O6_RequestDuration proves a finished request records
// http.server.request.duration with its route. Criterion 3.
func TestOtel_O6_RequestDuration(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithSpans(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(context.Background()), "GET unmatched")
	wlog.Set(unit, "kind", "request")
	wlog.Set(unit, "operation", "GET /orders/{id}")
	wlog.SetGroup(unit, "http", "method", "GET", "route", "/orders/{id}", "status", 200, "scheme", "https")
	end()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	point := firstHistogram(t, rm, "http.server.request.duration")
	route, ok := point.Attributes.Value(attribute.Key("http.route"))
	if !ok || route.AsString() != "/orders/{id}" {
		t.Errorf("http.route = %v, want /orders/{id}", point.Attributes.ToSlice())
	}
}

// TestOtel_O6_OtherOperation proves a kind past its cap records _OTHER, not the raw name.
func TestOtel_O6_OtherOperation(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithMaxOperations(1), wlogotel.WithSpans(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	m := p.(wlog.Measurer)
	ctx := context.Background()
	m.Measure(ctx, wlog.Measure{Kind: "job", Operation: "first"})
	m.Measure(ctx, wlog.Measure{Kind: "job", Operation: "second"})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !histogramHas(t, rm, "wlog.work.duration", "wlog.operation", "_OTHER") {
		t.Error("the capped operation was not recorded as _OTHER")
	}
	if histogramHas(t, rm, "wlog.work.duration", "wlog.operation", "second") {
		t.Error("the capped operation kept the raw name second")
	}
}

// TestOtel_O6_OtherMethod proves a method outside the nine standard methods records as _OTHER.
func TestOtel_O6_OtherMethod(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithSpans(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	p.(wlog.Measurer).Measure(context.Background(), wlog.Measure{Kind: "request", Method: "FOO", DurationMS: 1})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	point := firstHistogram(t, rm, "http.server.request.duration")
	method, ok := point.Attributes.Value(attribute.Key("http.request.method"))
	if !ok || method.AsString() != "_OTHER" {
		t.Errorf("method = %v, want _OTHER", point.Attributes.ToSlice())
	}
}

// TestOtel_O6_SkipsArrayGroups proves logs, errors, audit, and calls stay off the span.
func TestOtel_O6_SkipsArrayGroups(t *testing.T) {
	rec, ctx := recorder(t)
	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "op")
	wlog.Set(unit, "logs", []any{"line"})
	wlog.Set(unit, "errors", []any{"err"})
	wlog.Set(unit, "audit", []any{"row"})
	wlog.Set(unit, "calls", []any{"call"})
	wlog.Set(unit, "call_stats", "kept")
	end()

	attrs := rec.Started()[0].Attributes()
	for _, name := range []string{"logs", "errors", "audit", "calls"} {
		if _, ok := findAttr(attrs, name); ok {
			t.Errorf("the span holds %s", name)
		}
	}
	if _, ok := findAttr(attrs, "call_stats"); !ok {
		t.Error("the span dropped call_stats")
	}
}

// TestOtel_O6_StatusErrorType proves a 500 request with no error still sets error.type.
func TestOtel_O6_StatusErrorType(t *testing.T) {
	rec, ctx := recorder(t)
	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "GET /orders")
	wlog.Set(unit, "kind", "request")
	wlog.SetGroup(unit, "http", "status", 500)
	end()

	value, ok := findAttr(rec.Started()[0].Attributes(), "error.type")
	if !ok || value.AsString() != "500" {
		t.Errorf("error.type = %v present=%v, want 500", value, ok)
	}
}

// TestOtel_O6_TypedSlice proves a slice of one scalar type stays a typed slice.
func TestOtel_O6_TypedSlice(t *testing.T) {
	rec, ctx := recorder(t)
	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "op")
	wlog.Set(unit, "tags", []any{"a", "b"})
	end()

	value, ok := findAttr(rec.Started()[0].Attributes(), "tags")
	if !ok {
		t.Fatal("the span has no tags attribute")
	}
	if value.Type() != attribute.STRINGSLICE {
		t.Errorf("tags type = %v, want a string slice", value.Type())
	}
	got := value.AsStringSlice()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("tags = %v, want [a b]", got)
	}
}

// TestOtel_O6_NonRecordingSpan proves a span that does not record leaves the event ids alone.
func TestOtel_O6_NonRecordingSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()),
		sdktrace.WithSpanProcessor(rec),
	)
	ctx, span := tp.Tracer("test").Start(context.Background(), "server")
	defer span.End()

	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, events := wlogtest.New(t, wlog.WithPlugins(p))
	_, end := wlog.Start(log.WithContext(ctx), "op")
	end()

	group, _ := events.Last()["trace"].(map[string]any)
	if group["trace_id"] == span.SpanContext().TraceID().String() {
		t.Errorf("trace_id = %v, want the event's own id, not the non-recording span", group["trace_id"])
	}
}

// TestOtel_O6_WithSpansFalse proves WithSpans(false) writes no attribute on the span.
func TestOtel_O6_WithSpansFalse(t *testing.T) {
	rec, ctx := recorder(t)
	p, err := wlogotel.Plugin(wlogotel.WithSpans(false), wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "op")
	wlog.Set(unit, "note", "hello")
	end()

	if _, ok := findAttr(rec.Started()[0].Attributes(), "note"); ok {
		t.Error("WithSpans(false) wrote note onto the span")
	}
}

// TestOtel_O6_WithStats proves WithStats(false) exports no counter, and WithStats exports a drop.
func TestOtel_O6_WithStats(t *testing.T) {
	offReader := sdkmetric.NewManualReader()
	offMP := sdkmetric.NewMeterProvider(sdkmetric.WithReader(offReader))
	off, err := wlogotel.Plugin(wlogotel.WithMeterProvider(offMP), wlogotel.WithSpans(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	offLog, _ := wlogtest.New(t, wlog.WithPlugins(off))
	_, end := wlog.Start(offLog.WithContext(context.Background()), "op")
	end()
	var offRM metricdata.ResourceMetrics
	if err := offReader.Collect(context.Background(), &offRM); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if metricNamed(offRM, "wlog.events.emitted") {
		t.Error("WithStats(false) exported wlog.events.emitted")
	}

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	on, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithSpans(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(on), wlog.WithLevel(wlog.LevelError))
	_, end = wlog.Start(log.WithContext(context.Background()), "op")
	end()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumOf(t, rm, "wlog.events.dropped"); got != 1 {
		t.Errorf("wlog.events.dropped = %d, want 1", got)
	}
}

func histogramHas(t *testing.T, rm metricdata.ResourceMetrics, name, key, want string) bool {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s is not a float64 histogram", name)
			}
			for _, point := range hist.DataPoints {
				if value, ok := point.Attributes.Value(attribute.Key(key)); ok && value.AsString() == want {
					return true
				}
			}
		}
	}
	return false
}

func metricNamed(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}
