// This file checks the logrus input path: an entry with a context folds and writes no
// line, the levels map, the data fields land in the line, and an entry with no event
// reaches the wrapped logger.
package wloglogrus_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/jeremygprawira/wlog"
	wloglogrus "github.com/jeremygprawira/wlog/log/logrus"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLogrus_C3_Folds proves that an entry with a context folds and writes nothing.
func TestLogrus_C3_Folds(t *testing.T) {
	log, rec := wlogtest.New(t)
	buf := &bytes.Buffer{}
	logger := newLogger(buf)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger.WithContext(ctx).WithField("k", "v").Warn("hello")
	end()

	line := onlyLine(t, rec)
	if line["level"] != "warn" || line["msg"] != "hello" {
		t.Errorf("line = %v, want warn hello", line)
	}
	attrs, _ := line["attrs"].(map[string]any)
	if attrs["k"] != "v" {
		t.Errorf("attrs = %v, want k=v", attrs)
	}
	if buf.Len() != 0 {
		t.Errorf("the folded entry was written: %s", buf.String())
	}
}

// TestLogrus_C3_TraceFoldsAsDebug proves that trace folds as debug.
func TestLogrus_C3_TraceFoldsAsDebug(t *testing.T) {
	log, rec := wlogtest.New(t)
	logger := newLogger(&bytes.Buffer{})

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger.WithContext(ctx).Trace("verbose")
	end()

	if line := onlyLine(t, rec); line["level"] != "debug" {
		t.Errorf("line = %v, want a debug line", line)
	}
}

// TestLogrus_C3_ForwardsWithoutEvent proves that an entry with no event reaches the
// wrapped logger unchanged.
func TestLogrus_C3_ForwardsWithoutEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := newLogger(buf)

	logger.WithField("k", "v").Info("plain")

	if !strings.Contains(buf.String(), "plain") || !strings.Contains(buf.String(), `"k":"v"`) {
		t.Errorf("the wrapped logger wrote %q, want the plain entry", buf.String())
	}
}

// newLogger returns a JSON logger with the wlog hook installed.
func newLogger(buf *bytes.Buffer) *logrus.Logger {
	logger := logrus.New()
	logger.Out = buf
	logger.Formatter = &logrus.JSONFormatter{}
	logger.SetLevel(logrus.TraceLevel)
	wloglogrus.Install(logger)
	return logger
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
