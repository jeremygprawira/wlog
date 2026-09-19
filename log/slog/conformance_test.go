// This file proves the slog bridge against testing/slogtest for folded records, and
// against the shared log suite in both directions.
package wlogslog_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	logconformance "github.com/jeremygprawira/wlog/internal/conformance/log"
	wlogslog "github.com/jeremygprawira/wlog/log/slog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestSlogInput_Slogtest proves that the handler passes every case of testing/slogtest,
// with every record folded into one open event.
func TestSlogInput_Slogtest(t *testing.T) {
	log, _ := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	defer end()

	handler := func() slog.Handler {
		return foldedHandler{inner: wlogslog.Handler(slog.NewJSONHandler(io.Discard, nil)), ctx: ctx}
	}
	results := func() []map[string]any {
		raw, _ := wlog.Field(ctx, "logs")
		lines, _ := raw.([]any)
		out := make([]map[string]any, 0, len(lines))
		for _, item := range lines {
			line, _ := item.(map[string]any)
			out = append(out, slogtestResult(line))
		}
		return out
	}

	if err := slogtest.TestHandler(handler(), results); err != nil {
		t.Error(err)
	}
}

// slogtestResult turns one folded line into the output shape slogtest reads: the message,
// the level, the time of the record, and the attrs at the top level.
func slogtestResult(line map[string]any) map[string]any {
	result := map[string]any{
		slog.MessageKey: line["msg"],
		slog.LevelKey:   strings.ToUpper(fmt.Sprint(line["level"])),
	}
	if text, ok := line["time"].(string); ok && text != "" {
		result[slog.TimeKey] = text
	}
	if attrs, ok := line["attrs"].(map[string]any); ok {
		for key, value := range attrs {
			result[key] = value
		}
	}
	return result
}

// foldedHandler folds every record into one fixed event, because slogtest passes its own
// context to the handler.
type foldedHandler struct {
	inner slog.Handler
	ctx   context.Context
}

// Enabled defers to the wrapped handler with the fixed event context.
func (h foldedHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.inner.Enabled(h.ctx, level)
}

// Handle folds one record into the fixed event.
func (h foldedHandler) Handle(_ context.Context, r slog.Record) error {
	return h.inner.Handle(h.ctx, r)
}

// WithAttrs keeps the fixed event context.
func (h foldedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return foldedHandler{inner: h.inner.WithAttrs(attrs), ctx: h.ctx}
}

// WithGroup keeps the fixed event context.
func (h foldedHandler) WithGroup(name string) slog.Handler {
	return foldedHandler{inner: h.inner.WithGroup(name), ctx: h.ctx}
}

// TestSlog_Conformance proves that the bridge passes the shared log suite in both
// directions.
func TestSlog_Conformance(t *testing.T) {
	logconformance.Run(conformance.Tester{T: t}, slogFactory{})
}

// slogFactory builds the slog bridge in both directions over one collector.
type slogFactory struct{}

// Input logs one record through the handler.
func (slogFactory) Input(ctx context.Context, sink *logconformance.Collector, message string, attrs map[string]any) error {
	logger := slog.New(wlogslog.Handler(&collectorHandler{sink: sink}))
	args := make([]any, 0, len(attrs)*2)
	for key, value := range attrs {
		args = append(args, key, value)
	}
	logger.InfoContext(ctx, message, args...)
	return nil
}

// Output returns a Logger whose events go through the drain into the collector.
func (slogFactory) Output(sink *logconformance.Collector) *wlog.Logger {
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(wlogslog.Drain(&collectorHandler{sink: sink})))
}

// collectorHandler reports every record to one collector, with groups kept as maps.
type collectorHandler struct {
	sink *logconformance.Collector
}

// Enabled accepts every level.
func (h *collectorHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle reports one record.
func (h *collectorHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		addAttr(attrs, a)
		return true
	})
	h.sink.Record(r.Level.String(), r.Message, attrs)
	return nil
}

// WithAttrs returns the same handler, because the suite never sets them on the collector.
func (h *collectorHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup returns the same handler, because the suite never groups the collector.
func (h *collectorHandler) WithGroup(string) slog.Handler { return h }

// addAttr renders one attr into a map, nesting a group as a map of its own.
func addAttr(into map[string]any, a slog.Attr) {
	value := a.Value.Resolve()
	if value.Kind() != slog.KindGroup {
		into[a.Key] = value.Any()
		return
	}
	group := map[string]any{}
	for _, item := range value.Group() {
		addAttr(group, item)
	}
	if len(group) > 0 {
		into[a.Key] = group
	}
}
