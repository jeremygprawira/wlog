// This file runs the log conformance suite against a reference slog bridge, and against a
// broken bridge that must fail it.
package logconformance_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	logconformance "github.com/jeremygprawira/wlog/internal/conformance/log"
	wlogslog "github.com/jeremygprawira/wlog/log/slog"
)

// TestConformance_LogFake proves that the reference slog bridge passes every log scenario,
// and that a bridge which writes nothing fails them with a report.
func TestConformance_LogFake(t *testing.T) {
	logconformance.Run(conformance.Tester{T: t}, fake{})

	t.Run("BrokenFakeFails", func(t *testing.T) {
		broken := &conformance.Capture{}
		logconformance.Run(broken, brokenFactory{})

		if len(broken.Failures) == 0 {
			t.Fatal("the broken bridge passed the suite")
		}
		for _, name := range []string{"InputFolds", "OutputRecord"} {
			if !broken.Reports(name) {
				t.Errorf("the broken bridge did not fail %s:\n%s", name, strings.Join(broken.Failures, "\n"))
			}
		}
	})
}

// fake is the reference bridge: the slog handler for input, and the slog drain for output.
type fake struct{}

// Input logs one record through the slog handler, with the context of the unit of work.
func (fake) Input(ctx context.Context, sink *logconformance.Collector, message string, attrs map[string]any) error {
	logger := slog.New(wlogslog.Handler(sinkHandler{sink}))
	args := make([]any, 0, len(attrs)*2)
	for key, value := range attrs {
		args = append(args, key, value)
	}
	logger.InfoContext(ctx, message, args...)
	return nil
}

// Output returns a Logger whose every event becomes one slog record.
func (fake) Output(sink *logconformance.Collector) *wlog.Logger {
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(wlogslog.Drain(sinkHandler{sink})))
}

// brokenFactory writes nothing in either direction.
type brokenFactory struct{}

// Input logs nothing, and fails loudly.
func (brokenFactory) Input(context.Context, *logconformance.Collector, string, map[string]any) error {
	return errors.New("broken bridge")
}

// Output returns a Logger whose drain writes nowhere.
func (brokenFactory) Output(*logconformance.Collector) *wlog.Logger {
	sink := wlog.DrainFunc(func(context.Context, map[string]any) {})
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(sink))
}

// sinkHandler is a slog.Handler that reports each record to a collector, so the reference
// bridge needs no buffer and no JSON.
type sinkHandler struct{ sink *logconformance.Collector }

// Enabled accepts every level.
func (sinkHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle reports one record to the collector.
func (h sinkHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = attrValue(a)
		return true
	})
	h.sink.Record(r.Level.String(), r.Message, attrs)
	return nil
}

// WithAttrs keeps no state, because the fake logger sends no attrs that way.
func (h sinkHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup keeps no state, because the fake logger sends no group that way.
func (h sinkHandler) WithGroup(string) slog.Handler { return h }

// attrValue turns one slog attribute into a value, and a group into a nested map.
func attrValue(a slog.Attr) any {
	if a.Value.Kind() == slog.KindGroup {
		out := map[string]any{}
		for _, child := range a.Value.Group() {
			out[child.Key] = attrValue(child)
		}
		return out
	}
	return a.Value.Any()
}
