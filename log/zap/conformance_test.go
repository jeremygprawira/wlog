// This file runs the shared log suite against the zap bridge in both directions.
package wlogzap_test

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	logconformance "github.com/jeremygprawira/wlog/internal/conformance/log"
	wlogzap "github.com/jeremygprawira/wlog/log/zap"
)

// TestZap_Conformance proves that the zap bridge passes the shared log suite in both
// directions.
func TestZap_Conformance(t *testing.T) {
	logconformance.Run(conformance.Tester{T: t}, zapFactory{})
}

// zapFactory builds the zap bridge in both directions over one collector core.
type zapFactory struct{}

// Input logs one record through a bound logger.
func (zapFactory) Input(ctx context.Context, sink *logconformance.Collector, message string, attrs map[string]any) error {
	logger := zap.New(wlogzap.Core(&collectorCore{sink: sink}))
	wlogzap.Bind(ctx, logger).Info(message, fieldsOf(attrs)...)
	return nil
}

// Output returns a Logger whose events go through the drain into the collector.
func (zapFactory) Output(sink *logconformance.Collector) *wlog.Logger {
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(wlogzap.Drain(zap.New(&collectorCore{sink: sink}))))
}

// fieldsOf turns a map into the fields of one entry.
func fieldsOf(attrs map[string]any) []zap.Field {
	fields := make([]zap.Field, 0, len(attrs))
	for key, value := range attrs {
		fields = append(fields, zap.Any(key, value))
	}
	return fields
}

// collectorCore reports every written entry to one collector.
type collectorCore struct {
	sink   *logconformance.Collector
	fields []zapcore.Field
}

// Enabled accepts every level.
func (c *collectorCore) Enabled(zapcore.Level) bool { return true }

// With keeps the fields of the logger.
func (c *collectorCore) With(fields []zapcore.Field) zapcore.Core {
	combined := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	combined = append(combined, c.fields...)
	combined = append(combined, fields...)
	return &collectorCore{sink: c.sink, fields: combined}
}

// Check adds this core to the entry.
func (c *collectorCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !c.Enabled(ent.Level) {
		return ce
	}
	return ce.AddCore(ent, c)
}

// Write reports one entry to the collector.
func (c *collectorCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	encoder := zapcore.NewMapObjectEncoder()
	for _, field := range append(append([]zapcore.Field{}, c.fields...), fields...) {
		field.AddTo(encoder)
	}
	c.sink.Record(ent.Level.String(), ent.Message, encoder.Fields)
	return nil
}

// Sync writes nothing.
func (c *collectorCore) Sync() error { return nil }
