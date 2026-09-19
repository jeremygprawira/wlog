// This file runs the shared log suite against the zerolog bridge in both directions.
package wlogzerolog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	logconformance "github.com/jeremygprawira/wlog/internal/conformance/log"
	wlogzerolog "github.com/jeremygprawira/wlog/log/zerolog"
)

// TestZerolog_Conformance proves that the zerolog bridge passes the shared log suite in
// both directions.
func TestZerolog_Conformance(t *testing.T) {
	logconformance.Run(conformance.Tester{T: t}, zerologFactory{})
}

// zerologFactory builds the zerolog bridge in both directions over one collector.
type zerologFactory struct{}

// Input logs one line. Inside an event it uses the bound logger, and outside it uses the
// app's own logger, which is what the plugin gives an app.
func (zerologFactory) Input(ctx context.Context, sink *logconformance.Collector, message string, attrs map[string]any) error {
	logger := collectorLogger(sink)
	if wlog.HasEvent(ctx) {
		logger = wlogzerolog.Bind(ctx, logger)
	}
	event := logger.Info()
	for key, value := range attrs {
		event = event.Interface(key, value)
	}
	event.Msg(message)
	return nil
}

// Output returns a Logger whose events go through the drain into the collector.
func (zerologFactory) Output(sink *logconformance.Collector) *wlog.Logger {
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(wlogzerolog.Drain(collectorLogger(sink))))
}

// collectorLogger returns a JSON logger writing into one collector.
func collectorLogger(sink *logconformance.Collector) zerolog.Logger {
	return zerolog.New(&collectorWriter{sink: sink})
}

// collectorWriter parses each JSON line of zerolog into one collector record.
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
	level, _ := decoded["level"].(string)
	message, _ := decoded["message"].(string)
	for _, key := range []string{"level", "message", "time"} {
		delete(decoded, key)
	}
	w.sink.Record(level, message, decoded)
}
