// This file runs the shared log suite against the logr bridge in both directions.
package wloglogr_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	logconformance "github.com/jeremygprawira/wlog/internal/conformance/log"
	wloglogr "github.com/jeremygprawira/wlog/log/logr"
)

// TestLogr_Conformance proves that the logr bridge passes the shared log suite in both
// directions.
func TestLogr_Conformance(t *testing.T) {
	logconformance.Run(conformance.Tester{T: t}, logrFactory{})
}

// logrFactory builds the logr bridge in both directions over one collector.
type logrFactory struct{}

// Input logs one record through the collector logger. For a call inside an event it
// wraps that logger with Sink, which is what the plugin does at the start of a unit of
// work.
func (logrFactory) Input(ctx context.Context, sink *logconformance.Collector, message string, attrs map[string]any) error {
	base := logr.New(&collectorSink{sink: sink})
	logger := base
	if wlog.HasEvent(ctx) {
		logger = logr.New(wloglogr.Sink(ctx, base.GetSink()))
	}
	kv := make([]any, 0, len(attrs)*2)
	for key, value := range attrs {
		kv = append(kv, key, value)
	}
	logger.Info(message, kv...)
	return nil
}

// Output returns a Logger whose events go through the drain into the collector.
func (logrFactory) Output(sink *logconformance.Collector) *wlog.Logger {
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(wloglogr.Drain(logr.New(&collectorSink{sink: sink}))))
}

// collectorSink reports every call to one collector.
type collectorSink struct {
	sink   *logconformance.Collector
	values []any
}

// Init does nothing.
func (c *collectorSink) Init(logr.RuntimeInfo) {}

// Enabled accepts every level.
func (c *collectorSink) Enabled(int) bool { return true }

// Info reports one message.
func (c *collectorSink) Info(_ int, msg string, kv ...any) {
	c.sink.Record("info", msg, kvMap(append(append([]any{}, c.values...), kv...)))
}

// Error reports one error.
func (c *collectorSink) Error(_ error, msg string, kv ...any) {
	c.sink.Record("error", msg, kvMap(append(append([]any{}, c.values...), kv...)))
}

// WithValues keeps the pairs for the next call.
func (c *collectorSink) WithValues(kv ...any) logr.LogSink {
	return &collectorSink{sink: c.sink, values: append(append([]any{}, c.values...), kv...)}
}

// WithName returns the same sink, because the suite never names the collector.
func (c *collectorSink) WithName(string) logr.LogSink { return c }
