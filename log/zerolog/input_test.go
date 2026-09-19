// This file checks the zerolog input path: the hook folds level and message and discards
// the event, the bound logger folds its fields, and a line with no event reaches the
// base writer.
package wlogzerolog_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jeremygprawira/wlog"
	wlogzerolog "github.com/jeremygprawira/wlog/log/zerolog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestZerolog_C3_HookFolds proves that the hook folds level and message and discards the
// event, so no writer sees it and no field is read.
func TestZerolog_C3_HookFolds(t *testing.T) {
	log, rec := wlogtest.New(t)
	buf := &bytes.Buffer{}
	logger := zerolog.New(buf).Hook(wlogzerolog.Hook())

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger.Warn().Ctx(ctx).Str("k", "v").Msg("hello")
	end()

	line := onlyLine(t, rec)
	if line["level"] != "warn" || line["msg"] != "hello" {
		t.Errorf("line = %v, want warn hello", line)
	}
	if _, present := line["attrs"]; present {
		t.Errorf("attrs = %v, want none, because a hook reads no field", line["attrs"])
	}
	if buf.Len() != 0 {
		t.Errorf("the folded event was written: %s", buf.String())
	}
}

// TestZerolog_C3_PluginFoldsFields proves that the bound logger folds its fields.
func TestZerolog_C3_PluginFoldsFields(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithPlugins(wlogzerolog.Plugin(zerolog.New(&bytes.Buffer{}))))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	zerolog.Ctx(ctx).Info().Str("k", "v").Msg("hello")
	end()

	line := onlyLine(t, rec)
	if line["level"] != "info" || line["msg"] != "hello" {
		t.Errorf("line = %v, want info hello", line)
	}
	attrs, _ := line["attrs"].(map[string]any)
	if attrs["k"] != "v" {
		t.Errorf("attrs = %v, want k=v", attrs)
	}
}

// TestZerolog_C3_ForwardsWithoutEvent proves that a line with no event reaches the base
// writer.
func TestZerolog_C3_ForwardsWithoutEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := zerolog.New(buf).Hook(wlogzerolog.Hook())

	logger.Info().Str("k", "v").Msg("plain")

	if !strings.Contains(buf.String(), "plain") || !strings.Contains(buf.String(), `"k":"v"`) {
		t.Errorf("the base writer holds %q, want the plain line", buf.String())
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
