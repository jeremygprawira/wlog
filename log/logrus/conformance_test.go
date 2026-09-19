// This file runs the shared log suite against the logrus bridge in both directions.
package wloglogrus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	logconformance "github.com/jeremygprawira/wlog/internal/conformance/log"
	wloglogrus "github.com/jeremygprawira/wlog/log/logrus"
)

// TestLogrus_Conformance proves that the logrus bridge passes the shared log suite in
// both directions.
func TestLogrus_Conformance(t *testing.T) {
	logconformance.Run(conformance.Tester{T: t}, logrusFactory{})
}

// logrusFactory builds the logrus bridge in both directions over one collector.
type logrusFactory struct{}

// Input logs one entry with the context of the unit of work.
func (logrusFactory) Input(ctx context.Context, sink *logconformance.Collector, message string, attrs map[string]any) error {
	fields := logrus.Fields{}
	for key, value := range attrs {
		fields[key] = value
	}
	collectorLogger(sink).WithContext(ctx).WithFields(fields).Info(message)
	return nil
}

// Output returns a Logger whose events go through the drain into the collector.
func (logrusFactory) Output(sink *logconformance.Collector) *wlog.Logger {
	return wlog.New(wlog.WithSilent(), wlog.WithDrains(wloglogrus.Drain(collectorLogger(sink))))
}

// collectorLogger returns a JSON logger with the wlog hook, writing into one collector.
func collectorLogger(sink *logconformance.Collector) *logrus.Logger {
	logger := logrus.New()
	logger.Out = &collectorWriter{sink: sink}
	logger.Formatter = &logrus.JSONFormatter{}
	logger.SetLevel(logrus.TraceLevel)
	wloglogrus.Install(logger)
	return logger
}

// collectorWriter parses each JSON line of logrus into one collector record.
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
	message, _ := decoded["msg"].(string)
	for _, key := range []string{"level", "msg", "time"} {
		delete(decoded, key)
	}
	w.sink.Record(level, message, decoded)
}
