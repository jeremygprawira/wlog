package wlogotel_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
	wlogotel "github.com/jeremygprawira/wlog/trace/otel"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// recorder returns a span recorder and a context that holds a recording span.
func recorder(t *testing.T) (*tracetest.SpanRecorder, context.Context) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	ctx, _ := tp.Tracer("test").Start(context.Background(), "server")
	return rec, ctx
}

// findAttr returns one attribute of a span.
func findAttr(attrs []attribute.KeyValue, name string) (attribute.Value, bool) {
	for _, kv := range attrs {
		if string(kv.Key) == name {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// TestOtel_TraceIDs proves the Starter copies the recording span's ids onto the event,
// so the trace group names the OTel trace and not the generated one.
func TestOtel_TraceIDs(t *testing.T) {
	_, ctx := recorder(t)
	sc := oteltrace.SpanContextFromContext(ctx)

	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, events := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "op")
	_ = unit
	end()

	group, ok := events.Last()["trace"].(map[string]any)
	if !ok {
		t.Fatalf("event has no trace group: %v", events.Last()["trace"])
	}
	if group["trace_id"] != sc.TraceID().String() {
		t.Errorf("trace_id = %v, want the span's own id %s", group["trace_id"], sc.TraceID())
	}
	if group["span_id"] != sc.SpanID().String() {
		t.Errorf("span_id = %v, want the span's own id %s", group["span_id"], sc.SpanID())
	}
}

// TestOtel_SpanAttributes proves the Finisher copies the redacted event onto the span,
// sets the status from outcome, and adds one exception event.
func TestOtel_SpanAttributes(t *testing.T) {
	rec, ctx := recorder(t)

	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "GET /orders")
	wlog.SetGroup(unit, "http", "method", "GET", "route", "/orders", "status", 502)
	wlog.Error(unit, errors.New("boom"))
	end()

	spans := rec.Started()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status().Code)
	}
	if span.Status().Description != "boom" {
		t.Errorf("span status description = %q, want boom", span.Status().Description)
	}
	attrs := span.Attributes()
	for name, want := range map[string]string{
		"http.request.method":       "GET",
		"http.route":                "/orders",
		"http.response.status_code": "502",
	} {
		value, ok := findAttr(attrs, name)
		if !ok {
			t.Errorf("span has no %s attribute", name)
			continue
		}
		if got := value.Emit(); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	if _, ok := findAttr(attrs, "logs"); ok {
		t.Error("the span holds a logs attribute")
	}
	if _, ok := findAttr(attrs, "calls"); ok {
		t.Error("the span holds a calls attribute")
	}
	events := span.Events()
	if len(events) != 1 || events[0].Name != "exception" {
		t.Fatalf("span events = %v, want one exception", events)
	}
	if value, ok := findAttr(events[0].Attributes, "exception.message"); !ok || value.AsString() != "boom" {
		t.Errorf("exception event = %v, want exception.message boom", events[0].Attributes)
	}
}

// TestOtel_Metrics proves the Measurer records the two duration histograms with the
// semconv attribute names.
func TestOtel_Metrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithSpans(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	m, ok := p.(wlog.Measurer)
	if !ok {
		t.Fatal("the plugin is not a Measurer")
	}
	ctx := context.Background()
	m.Measure(ctx, wlog.Measure{Kind: "request", Method: "GET", Scheme: "https", Status: "502", Route: "/orders", DurationMS: 42})
	m.Measure(ctx, wlog.Measure{Kind: "job", Operation: "cleanup", Outcome: "success", System: "cron", DurationMS: 1000})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	request := firstHistogram(t, rm, "http.server.request.duration")
	if request.Count != 1 {
		t.Errorf("request histogram count = %d, want 1", request.Count)
	}
	if method, ok := request.Attributes.Value(attribute.Key("http.request.method")); !ok || method.AsString() != "GET" {
		t.Errorf("request method = %v, want GET", request.Attributes.ToSlice())
	}
	work := firstHistogram(t, rm, "wlog.work.duration")
	if work.Count != 1 {
		t.Errorf("work histogram count = %d, want 1", work.Count)
	}
	if kind, ok := work.Attributes.Value(attribute.Key("wlog.kind")); !ok || kind.AsString() != "job" {
		t.Errorf("work kind = %v, want job", work.Attributes.ToSlice())
	}
}

// TestOtel_OperationCap proves a kind past its cap records as _OTHER and reports
// WLOG_CAP_REACHED once.
func TestOtel_OperationCap(t *testing.T) {
	var mu sync.Mutex
	var problems []wlog.Problem
	p, err := wlogotel.Plugin(wlogotel.WithMaxOperations(2), wlogotel.WithSpans(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	wlogtest.New(t, wlog.WithPlugins(p), wlog.OnProblem(func(pr wlog.Problem) {
		mu.Lock()
		problems = append(problems, pr)
		mu.Unlock()
	}))
	m := p.(wlog.Measurer)
	ctx := context.Background()
	for _, op := range []string{"a", "b", "c", "d"} {
		m.Measure(ctx, wlog.Measure{Kind: "job", Operation: op})
	}

	mu.Lock()
	defer mu.Unlock()
	caps := 0
	for _, pr := range problems {
		if pr.Code == "WLOG_CAP_REACHED" {
			caps++
		}
	}
	if caps != 1 {
		t.Errorf("WLOG_CAP_REACHED reported %d times, want 1", caps)
	}
}

// TestOtel_Stats proves the observable counters read the Logger's Stats on a scrape.
func TestOtel_Stats(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p, err := wlogotel.Plugin(wlogotel.WithMeterProvider(mp), wlogotel.WithSpans(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	end()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := sumOf(t, rm, "wlog.events.emitted"); got != 1 {
		t.Errorf("wlog.events.emitted = %d, want 1", got)
	}
}

// TestOtel_MaskedFieldStaysMasked proves the Finisher sees the redacted event, so a
// secret never reaches a span.
func TestOtel_MaskedFieldStaysMasked(t *testing.T) {
	rec, ctx := recorder(t)
	p, err := wlogotel.Plugin(wlogotel.WithMetrics(false), wlogotel.WithStats(false))
	if err != nil {
		t.Fatalf("Plugin: %v", err)
	}
	log, _ := wlogtest.New(t, wlog.WithPlugins(p))
	unit, end := wlog.Start(log.WithContext(ctx), "op")
	wlog.Set(unit, "password", "hunter2")
	end()

	value, ok := findAttr(rec.Started()[0].Attributes(), "password")
	if !ok {
		t.Fatal("the span has no password attribute")
	}
	if value.AsString() == "hunter2" {
		t.Error("the secret reached the span")
	}
}

// firstHistogram returns the first data point of one float64 histogram.
func firstHistogram(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.HistogramDataPoint[float64] {
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
			if len(hist.DataPoints) == 0 {
				t.Fatalf("%s has no data points", name)
			}
			return hist.DataPoints[0]
		}
	}
	t.Fatalf("no metric named %s", name)
	return metricdata.HistogramDataPoint[float64]{}
}

// sumOf returns the total of one int64 sum instrument.
func sumOf(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is not an int64 sum", name)
			}
			var total int64
			for _, point := range sum.DataPoints {
				total += point.Value
			}
			return total
		}
	}
	t.Fatalf("no metric named %s", name)
	return 0
}
