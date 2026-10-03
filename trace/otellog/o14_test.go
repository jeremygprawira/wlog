package wlogotellog_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	oteltrace "go.opentelemetry.io/otel/trace"

	wlogotellog "github.com/jeremygprawira/wlog/trace/otellog"
)

// spanSeenLogger records the trace id Enabled saw.
type spanSeenLogger struct {
	embedded.Logger
	traceID oteltrace.TraceID
}

func (l *spanSeenLogger) Enabled(ctx context.Context, _ log.EnabledParameters) bool {
	l.traceID = oteltrace.SpanContextFromContext(ctx).TraceID()
	return true
}

func (l *spanSeenLogger) Emit(context.Context, log.Record) {}

type spanSeenProvider struct {
	embedded.LoggerProvider
	logger *spanSeenLogger
}

func (p *spanSeenProvider) Logger(string, ...log.LoggerOption) log.Logger { return p.logger }

// TestOtellog_O14_EnabledSeesEventSpan proves Enabled gets the span built from the event.
func TestOtellog_O14_EnabledSeesEventSpan(t *testing.T) {
	logger := &spanSeenLogger{}
	drain := wlogotellog.New(&spanSeenProvider{logger: logger})

	drain.Send(context.Background(), errorRequest())

	if logger.traceID.String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("Enabled saw trace id %s, want the event span", logger.traceID)
	}
}
