package wlogslog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogslog "github.com/jeremygprawira/wlog/log/slog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// logViaDefault stands in for a third-party library that logs through the global
// slog.Default(), so the test proves SetDefault with this Handler is safe globally.
func logViaDefault(ctx context.Context) {
	slog.Default().InfoContext(ctx, "from library", "lib", "ok")
}

// TestSlogInput_FoldsRecordIntoLogs proves a slog call made with an event-carrying ctx
// lands in that event's logs[], with level, message and attrs intact.
func TestSlogInput_FoldsRecordIntoLogs(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := wlogslog.Handler(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	logger := slog.New(handler)

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	logger.InfoContext(ctx, "hello", "k", "v")
	end()

	logs, _ := rec.Last()["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want exactly one folded line", rec.Last()["logs"])
	}
	line, _ := logs[0].(map[string]any)
	if line["msg"] != "hello" || line["level"] != "info" {
		t.Errorf("folded line = %v, want level=info msg=hello", line)
	}
	attrs, _ := line["attrs"].(map[string]any)
	if attrs["k"] != "v" {
		t.Errorf("folded attrs = %v, want k=v", line["attrs"])
	}
}

// TestSlogInput_PassthroughWithoutEvent proves a record with no event in ctx reaches
// the wrapped handler unchanged, so wiring this globally never swallows a log line.
func TestSlogInput_PassthroughWithoutEvent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(wlogslog.Handler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("plain line")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("passthrough output not a JSON record: %v\n%q", err, buf.String())
	}
	if got["msg"] != "plain line" {
		t.Errorf("passthrough msg = %v, want plain line", got["msg"])
	}
}

// TestSlogInput_DefaultHandlerAndGroups proves SetDefault routes a library call into
// the event, and that WithGroup/WithAttrs nesting survives into logs[].
func TestSlogInput_DefaultHandlerAndGroups(t *testing.T) {
	log, rec := wlogtest.New(t)
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	slog.SetDefault(slog.New(wlogslog.Handler(slog.NewJSONHandler(&bytes.Buffer{}, nil)).
		WithGroup("req").WithAttrs([]slog.Attr{slog.String("id", "42")})))
	logViaDefault(ctx)
	end()

	logs, _ := rec.Last()["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("logs = %v, want one line", rec.Last()["logs"])
	}
	line, _ := logs[0].(map[string]any)
	attrs, _ := line["attrs"].(map[string]any)
	req, _ := attrs["req"].(map[string]any)
	if req["id"] != "42" || req["lib"] != "ok" {
		t.Errorf("nested attrs = %v, want req.id=42 req.lib=ok", attrs)
	}
}
