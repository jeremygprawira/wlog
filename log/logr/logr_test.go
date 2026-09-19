// This file checks the logr bridge: the plugin folds Info and Error into the event, the
// verbosity and the names map, the sink forwards with no event, the slog path folds, and
// the drain writes each event as one record.
package wloglogr_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/go-logr/logr"

	"github.com/jeremygprawira/wlog"
	wloglogr "github.com/jeremygprawira/wlog/log/logr"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLogr_C2_PluginFolds proves that the plugin binds a logger whose calls fold, with
// V(0) as info and V(2) as debug.
func TestLogr_C2_PluginFolds(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(wloglogr.Plugin()))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger := logr.FromContextOrDiscard(ctx)
	logger.V(2).Info("verbose", "k", "v")
	logger.Info("plain", "k", "v")
	end()

	lines := logsOf(t, rec)
	if len(lines) != 2 {
		t.Fatalf("logs = %v, want two folded lines", rec.Last()["logs"])
	}
	if lines[0]["level"] != "debug" || lines[0]["msg"] != "verbose" {
		t.Errorf("line 0 = %v, want debug verbose", lines[0])
	}
	if lines[1]["level"] != "info" || lines[1]["msg"] != "plain" {
		t.Errorf("line 1 = %v, want info plain", lines[1])
	}
	attrs, _ := lines[1]["attrs"].(map[string]any)
	if attrs["k"] != "v" {
		t.Errorf("attrs = %v, want k=v", attrs)
	}
}

// TestLogr_C2_ErrorAndNames proves that Error folds with the error message, a nil error
// is allowed, and names join under logger.
func TestLogr_C2_ErrorAndNames(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(wloglogr.Plugin()))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger := logr.FromContextOrDiscard(ctx)
	logger.Error(errors.New("boom"), "failed", "k", "v")
	logger.WithName("a").WithName("b").Info("named")
	logger.Error(nil, "without error")
	end()

	lines := logsOf(t, rec)
	if len(lines) != 3 {
		t.Fatalf("logs = %v, want three folded lines", rec.Last()["logs"])
	}
	if lines[0]["level"] != "error" || lines[0]["msg"] != "failed" {
		t.Errorf("line 0 = %v, want error failed", lines[0])
	}
	attrs, _ := lines[0]["attrs"].(map[string]any)
	if attrs["error"] != "boom" {
		t.Errorf("attrs.error = %v, want the message of the error", attrs["error"])
	}
	named, _ := lines[1]["attrs"].(map[string]any)
	if named["logger"] != "a/b" {
		t.Errorf("attrs.logger = %v, want a/b", named["logger"])
	}
	if lines[2]["level"] != "error" {
		t.Errorf("line 2 = %v, want an error line with a nil error", lines[2])
	}
}

// TestLogr_C2_ForwardsWithoutEvent proves that a call with no event reaches the next
// sink unchanged.
func TestLogr_C2_ForwardsWithoutEvent(t *testing.T) {
	recorder := &recordingSink{}
	logger := logr.New(wloglogr.Sink(context.Background(), recorder))

	logger.Info("plain", "k", "v")
	logger.Error(errors.New("boom"), "failed")

	if recorder.infos != 1 || recorder.errors != 1 {
		t.Errorf("the next sink saw %d infos and %d errors, want 1 and 1", recorder.infos, recorder.errors)
	}
}

// TestLogr_C2_SlogHandler proves that logr.ToSlogHandler keeps the context and folds
// through the sink.
func TestLogr_C2_SlogHandler(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(wloglogr.Plugin()))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger := logr.FromContextOrDiscard(ctx)
	slog.New(logr.ToSlogHandler(logger)).Info("via slog", "k", "v")
	end()

	line := logsOf(t, rec)[0]
	if line["level"] != "info" || line["msg"] != "via slog" {
		t.Errorf("line = %v, want info via slog", line)
	}
	attrs, _ := line["attrs"].(map[string]any)
	if attrs["k"] != "v" {
		t.Errorf("attrs = %v, want k=v", attrs)
	}
}

// TestLogr_C2_Drain proves that one event becomes one logr record with the operation as
// the message.
func TestLogr_C2_Drain(t *testing.T) {
	recorder := &recordingSink{}
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(wloglogr.Drain(logr.New(recorder))))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "checkout")
	wlog.Set(ctx, "user", "u-1")
	end()
	_ = log.Flush(context.Background())

	if recorder.message != "checkout" {
		t.Errorf("message = %q, want checkout", recorder.message)
	}
	if recorder.attrs["user"] != "u-1" {
		t.Errorf("attrs = %v, want user=u-1", recorder.attrs)
	}
}

// recordingSink is a logr sink that remembers the calls it received.
type recordingSink struct {
	mu      sync.Mutex
	infos   int
	errors  int
	message string
	attrs   map[string]any
}

// Init does nothing.
func (r *recordingSink) Init(logr.RuntimeInfo) {}

// Enabled accepts every level.
func (r *recordingSink) Enabled(int) bool { return true }

// Info counts one message.
func (r *recordingSink) Info(_ int, msg string, kv ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.infos++
	r.message = msg
	r.attrs = kvMap(kv)
}

// Error counts one error.
func (r *recordingSink) Error(_ error, msg string, kv ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors++
	r.message = msg
	r.attrs = kvMap(kv)
}

// WithValues returns the same sink.
func (r *recordingSink) WithValues(...any) logr.LogSink { return r }

// WithName returns the same sink.
func (r *recordingSink) WithName(string) logr.LogSink { return r }

// kvMap turns pairs into a map.
func kvMap(kv []any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if key, ok := kv[i].(string); ok {
			out[key] = kv[i+1]
		}
	}
	return out
}

// logsOf returns the folded lines of the last event.
func logsOf(t *testing.T, rec *wlogtest.Recorder) []map[string]any {
	t.Helper()
	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	raw, _ := rec.Last()["logs"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		line, _ := item.(map[string]any)
		out = append(out, line)
	}
	return out
}
