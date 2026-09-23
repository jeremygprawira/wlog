package wlogotellog_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/log/logtest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
	wlogotellog "github.com/jeremygprawira/wlog/trace/otellog"
)

// captureExporter keeps every record the provider exports.
type captureExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

// Export copies the records, because the SDK keeps ownership of the slice.
func (e *captureExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, record := range records {
		e.records = append(e.records, record.Clone())
	}
	return nil
}

// Shutdown releases nothing.
func (e *captureExporter) Shutdown(context.Context) error { return nil }

// ForceFlush sends nothing, because a simple processor exports at once.
func (e *captureExporter) ForceFlush(context.Context) error { return nil }

// snapshot returns a copy of the exported records.
func (e *captureExporter) snapshot() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]sdklog.Record(nil), e.records...)
}

// providerFor builds a provider that exports through a capturing exporter.
func providerFor(exporter *captureExporter) log.LoggerProvider {
	return sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
}

// errorRequest returns one error request event.
func errorRequest() map[string]any {
	return map[string]any{
		"timestamp": "2026-09-22T10:00:00Z",
		"level":     "error",
		"summary":   "GET /orders failed",
		"kind":      "request",
		"operation": "GET /orders",
		"outcome":   "error",
		"http":      map[string]any{"method": "GET", "route": "/orders", "status": int64(502)},
		"trace": map[string]any{
			"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
			"span_id":  "00f067aa0ba902b7",
		},
	}
}

// TestOtelLog_ErrorRequest proves one error request becomes one record with severity 17,
// the event name, the summary as its body, and the event's trace id.
func TestOtelLog_ErrorRequest(t *testing.T) {
	exporter := &captureExporter{}
	drain := wlogotellog.New(providerFor(exporter))

	drain.Send(context.Background(), errorRequest())

	records := exporter.snapshot()
	if len(records) != 1 {
		t.Fatalf("exported %d records, want 1", len(records))
	}
	got := records[0]
	want := logtest.RecordFactory{
		EventName:    "wlog.request",
		Severity:     log.SeverityError,
		SeverityText: log.SeverityError.String(),
		Body:         attribute.StringValue("GET /orders failed"),
	}.NewRecord()

	if got.EventName() != want.EventName() {
		t.Errorf("event name = %q, want %q", got.EventName(), want.EventName())
	}
	if got.Severity() != want.Severity() {
		t.Errorf("severity = %d, want %d", got.Severity(), want.Severity())
	}
	if got.SeverityText() != want.SeverityText() {
		t.Errorf("severity text = %q, want %q", got.SeverityText(), want.SeverityText())
	}
	if got.Body().AsString() != want.Body().AsString() {
		t.Errorf("body = %q, want %q", got.Body().AsString(), want.Body().AsString())
	}
	if got.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id = %s, want the event's trace id", got.TraceID())
	}
	attrs := map[string]attribute.Value{}
	got.WalkAttributes(func(kv attribute.KeyValue) bool {
		attrs[string(kv.Key)] = kv.Value
		return true
	})
	if method, ok := attrs["http.request.method"]; !ok || method.AsString() != "GET" {
		t.Errorf("http.request.method = %v, want GET", attrs)
	}
}

// TestOtelLog_Timestamp proves the record takes its timestamp from the event.
func TestOtelLog_Timestamp(t *testing.T) {
	exporter := &captureExporter{}
	drain := wlogotellog.New(providerFor(exporter))

	drain.Send(context.Background(), errorRequest())

	want, err := time.Parse(time.RFC3339Nano, "2026-09-22T10:00:00Z")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := exporter.snapshot()[0].Timestamp(); !got.Equal(want) {
		t.Errorf("timestamp = %v, want %v", got, want)
	}
}

// TestOtelLog_ContextSpanWins proves a valid span on the context sets the record's trace,
// and the event's own ids do not replace it.
func TestOtelLog_ContextSpanWins(t *testing.T) {
	exporter := &captureExporter{}
	drain := wlogotellog.New(providerFor(exporter))

	traceID, err := oteltrace.TraceIDFromHex("11111111111111111111111111111111")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	spanID, err := oteltrace.SpanIDFromHex("2222222222222222")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}
	ctx := oteltrace.ContextWithSpanContext(context.Background(),
		oteltrace.NewSpanContext(oteltrace.SpanContextConfig{TraceID: traceID, SpanID: spanID}))

	drain.Send(ctx, errorRequest())

	if got := exporter.snapshot()[0].TraceID(); got != traceID {
		t.Errorf("trace id = %s, want the span's own id %s", got, traceID)
	}
}

// TestOtelLog_ServiceAttributes proves the resource keys join the record only with
// WithServiceAttributes(true).
func TestOtelLog_ServiceAttributes(t *testing.T) {
	event := errorRequest()
	event["service"] = map[string]any{"name": "checkout", "version": "1.4.2"}

	plain := &captureExporter{}
	wlogotellog.New(providerFor(plain)).Send(context.Background(), event)
	if _, ok := attributesOf(plain.snapshot()[0])["service.name"]; ok {
		t.Error("service.name reached the record without WithServiceAttributes(true)")
	}

	withService := &captureExporter{}
	wlogotellog.New(providerFor(withService), wlogotellog.WithServiceAttributes(true)).
		Send(context.Background(), event)
	if _, ok := attributesOf(withService.snapshot()[0])["service.name"]; !ok {
		t.Error("service.name is missing with WithServiceAttributes(true)")
	}
}

// TestOtelLog_MaskedFieldNeverLeaks proves core redacts before the drain, so a secret
// under a denied key never reaches a record.
func TestOtelLog_MaskedFieldNeverLeaks(t *testing.T) {
	exporter := &captureExporter{}
	drain := wlogotellog.New(providerFor(exporter))
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(drain))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Set(ctx, "conformance_marker", "PIPE25-MARKER")
	wlog.Set(ctx, "password", "PIPE25-s3cret")
	end()
	if err := log.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	records := exporter.snapshot()
	if len(records) != 1 {
		t.Fatalf("exported %d records, want 1", len(records))
	}
	attrs := attributesOf(records[0])
	if value, ok := attrs["password"]; ok && value.AsString() == "PIPE25-s3cret" {
		t.Error("the secret reached the record")
	}
	if value, ok := attrs["conformance_marker"]; !ok || value.AsString() != "PIPE25-MARKER" {
		t.Errorf("conformance_marker = %v, want the marker", attrs)
	}
}

// attributesOf reads the attributes of one record into a map.
func attributesOf(record sdklog.Record) map[string]attribute.Value {
	out := map[string]attribute.Value{}
	record.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = kv.Value
		return true
	})
	return out
}

// disabledLogger records nothing and reports every record as disabled.
type disabledLogger struct {
	embedded.Logger
	emits int
}

// Enabled reports false, so Send skips the record.
func (l *disabledLogger) Enabled(context.Context, log.EnabledParameters) bool { return false }

// Emit counts a record that should never arrive.
func (l *disabledLogger) Emit(context.Context, log.Record) { l.emits++ }

// disabledProvider returns the disabled logger.
type disabledProvider struct {
	embedded.LoggerProvider
	logger *disabledLogger
}

// Logger returns the one disabled logger.
func (p *disabledProvider) Logger(string, ...log.LoggerOption) log.Logger { return p.logger }

// TestOtelLog_DisabledSkips proves a disabled record is never built or emitted.
func TestOtelLog_DisabledSkips(t *testing.T) {
	logger := &disabledLogger{}
	drain := wlogotellog.New(&disabledProvider{logger: logger})

	drain.Send(context.Background(), errorRequest())

	if logger.emits != 0 {
		t.Errorf("emitted %d records, want 0 for a disabled record", logger.emits)
	}
}
