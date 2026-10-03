package wlogotellog_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"

	wlogotellog "github.com/jeremygprawira/wlog/trace/otellog"
)

// stampLogger keeps the observed timestamp of the record Send built.
type stampLogger struct {
	embedded.Logger
	zero   bool
	called bool
}

func (l *stampLogger) Enabled(context.Context, log.EnabledParameters) bool { return true }

func (l *stampLogger) Emit(_ context.Context, record log.Record) {
	l.called = true
	l.zero = record.ObservedTimestamp().IsZero()
}

// stampProvider returns the one logger under test.
type stampProvider struct {
	embedded.LoggerProvider
	logger *stampLogger
}

func (p *stampProvider) Logger(string, ...log.LoggerOption) log.Logger { return p.logger }

// TestOtelLog_O6_ObservedTimestamp proves Send sets the observed timestamp before emit.
func TestOtelLog_O6_ObservedTimestamp(t *testing.T) {
	logger := &stampLogger{}
	wlogotellog.New(&stampProvider{logger: logger}).Send(context.Background(), errorRequest())
	if !logger.called {
		t.Fatal("Send emitted no record")
	}
	if logger.zero {
		t.Error("observed timestamp is zero")
	}
}
