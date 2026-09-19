// This file checks the hclog bridge: the plugin folds a named logger with its pairs, the
// level checks answer true during the event, a call with no event reaches the base
// logger, and the drain writes one record per event.
package wloghclog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	logconformance "github.com/jeremygprawira/wlog/internal/conformance/log"
	wloghclog "github.com/jeremygprawira/wlog/log/hclog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHclog_C3_PluginFolds proves that a named logger folds with its pairs.
func TestHclog_C3_PluginFolds(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(wloghclog.Plugin(hclog.New(&hclog.LoggerOptions{Output: io.Discard, Level: hclog.Trace}))))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	hclog.FromContext(ctx).Named("db").With("component", "writer").Debug("query done", "k", "v")
	end()

	line := onlyLine(t, rec)
	if line["level"] != "debug" || line["msg"] != "query done" {
		t.Errorf("line = %v, want debug query done", line)
	}
	attrs, _ := line["attrs"].(map[string]any)
	for key, want := range map[string]any{"k": "v", "component": "writer", "logger": "db"} {
		if attrs[key] != want {
			t.Errorf("attrs.%s = %v, want %v", key, attrs[key], want)
		}
	}
}

// TestHclog_C3_LevelChecks proves that every level check answers true during the event
// and uses the base logger after it.
func TestHclog_C3_LevelChecks(t *testing.T) {
	log, _ := wlogtest.New(t, wlog.WithPlugins(wloghclog.Plugin(hclog.NewNullLogger())))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger := hclog.FromContext(ctx)
	if !logger.IsTrace() || !logger.IsDebug() || !logger.IsInfo() || !logger.IsWarn() || !logger.IsError() {
		t.Error("a level check answered false during the event, want true")
	}
	end()
	if logger.IsDebug() {
		t.Error("IsDebug = true after the event, and the null logger is off")
	}
}

// TestHclog_C3_ForwardsWithoutEvent proves that a call with no event reaches the base
// logger.
func TestHclog_C3_ForwardsWithoutEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	base := hclog.New(&hclog.LoggerOptions{Output: buf, JSONFormat: true, Level: hclog.Trace})

	wloghclog.Bind(context.Background(), base).Info("plain", "k", "v")

	if !strings.Contains(buf.String(), "plain") || !strings.Contains(buf.String(), `"k":"v"`) {
		t.Errorf("the base logger wrote %q, want the plain line with its pair", buf.String())
	}
}

// TestHclog_C3_Drain proves that one event becomes one hclog record.
func TestHclog_C3_Drain(t *testing.T) {
	buf := &bytes.Buffer{}
	base := hclog.New(&hclog.LoggerOptions{Output: buf, JSONFormat: true, Level: hclog.Trace})
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(wloghclog.Drain(base)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "checkout")
	wlog.Set(ctx, "user", "u-1")
	end()
	_ = log.Flush(context.Background())

	if !strings.Contains(buf.String(), "checkout") || !strings.Contains(buf.String(), `"user":"u-1"`) {
		t.Errorf("the drain wrote %q, want the checkout record", buf.String())
	}
}

// onlyLine returns the only folded line of the last event.
func onlyLine(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	logs, _ := rec.Last()["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want one folded line", rec.Last()["logs"])
	}
	line, _ := logs[0].(map[string]any)
	return line
}

// TestHclog_Conformance proves that the bridge passes the shared log suite in both
// directions.
func TestHclog_Conformance(t *testing.T) {
	logconformance.Run(conformance.Tester{T: t}, hclogFactory{})
}

// hclogFactory builds the hclog bridge in both directions over one collector.
type hclogFactory struct{}

// Input logs one record through a logger bound to the context.
func (hclogFactory) Input(ctx context.Context, sink *logconformance.Collector, message string, attrs map[string]any) error {
	ctx = hclog.WithContext(ctx, wloghclog.Bind(ctx, collectorLogger(sink)))
	args := make([]any, 0, len(attrs)*2)
	for key, value := range attrs {
		args = append(args, key, value)
	}
	hclog.FromContext(ctx).Info(message, args...)
	return nil
}

// Output returns a Logger whose events go through the drain into the collector.
func (hclogFactory) Output(sink *logconformance.Collector) *wlog.Logger {
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(wloghclog.Drain(collectorLogger(sink))))
}

// collectorLogger returns an hclog logger whose JSON lines reach one collector.
func collectorLogger(sink *logconformance.Collector) hclog.Logger {
	return hclog.New(&hclog.LoggerOptions{
		Output:     &collectorWriter{sink: sink},
		JSONFormat: true,
		Level:      hclog.Trace,
	})
}

// collectorWriter parses each JSON line of hclog into one collector record.
type collectorWriter struct {
	sink *logconformance.Collector
	line []byte
}

// Write buffers the bytes and reports every complete line.
func (w *collectorWriter) Write(p []byte) (int, error) {
	w.line = append(w.line, p...)
	for {
		index := bytes.IndexByte(w.line, '\n')
		if index < 0 {
			return len(p), nil
		}
		w.record(w.line[:index])
		w.line = w.line[index+1:]
	}
}

// record reports one JSON line to the collector.
func (w *collectorWriter) record(raw []byte) {
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return
	}
	level, _ := decoded["@level"].(string)
	message, _ := decoded["@message"].(string)
	for _, key := range []string{"@level", "@message", "@timestamp", "@module"} {
		delete(decoded, key)
	}
	w.sink.Record(level, message, decoded)
}
