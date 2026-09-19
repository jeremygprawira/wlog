// This file checks the zap input core: an entry carrying a context folds into the open
// event, the context field never reaches a sink, a sampler still decides with no event,
// and the plugin binds a logger the app can look up.
package wlogzap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jeremygprawira/wlog"
	wlogzap "github.com/jeremygprawira/wlog/log/zap"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestZap_C2_InputFolds proves that an entry carrying a context folds into the event and
// never reaches the wrapped core.
func TestZap_C2_InputFolds(t *testing.T) {
	log, rec := wlogtest.New(t)
	buf := &bytes.Buffer{}
	logger := zap.New(wlogzap.Core(jsonCore(buf, zapcore.DebugLevel)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger.Info("hello", zap.Any("ctx", ctx), zap.String("k", "v"), zap.Error(errors.New("boom")))
	end()

	line := onlyLine(t, rec)
	if line["level"] != "info" || line["msg"] != "hello" {
		t.Errorf("line = %v, want level info and msg hello", line)
	}
	attrs, _ := line["attrs"].(map[string]any)
	if attrs["k"] != "v" {
		t.Errorf("attrs = %v, want k=v", attrs)
	}
	if attrs["error"] != "boom" {
		t.Errorf("attrs.error = %v, want the message of the error", attrs["error"])
	}
	if buf.Len() != 0 {
		t.Errorf("the folded entry reached the wrapped core: %s", buf.String())
	}
}

// TestZap_C2_ContextNeverLeaks proves that the context field is removed on both paths.
func TestZap_C2_ContextNeverLeaks(t *testing.T) {
	log, rec := wlogtest.New(t)
	buf := &bytes.Buffer{}
	logger := zap.New(wlogzap.Core(jsonCore(buf, zapcore.DebugLevel)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger.Info("folded", zap.Any("ctx", ctx), zap.String("k", "v"))
	logger.Info("plain", zap.Any("ctx", context.Background()), zap.String("k", "v"))
	end()

	line := onlyLine(t, rec)
	attrs, _ := line["attrs"].(map[string]any)
	if _, present := attrs["ctx"]; present {
		t.Errorf("the folded attrs hold the context: %v", attrs)
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "context.Background") {
		t.Errorf("the context reached the event: %s", body)
	}
	if strings.Contains(buf.String(), `"ctx"`) {
		t.Errorf("the context reached the sink: %s", buf.String())
	}
	var written map[string]any
	if err := json.Unmarshal(buf.Bytes(), &written); err != nil {
		t.Fatalf("the plain entry is not JSON: %v", err)
	}
	if written["msg"] != "plain" || written["k"] != "v" {
		t.Errorf("the plain entry = %v, want the message and the field", written)
	}
}

// TestZap_C2_SamplerStillWrites proves that a sampler inside the wrapper still decides
// for an entry with no event.
func TestZap_C2_SamplerStillWrites(t *testing.T) {
	buf := &bytes.Buffer{}
	sampler := zapcore.NewSamplerWithOptions(jsonCore(buf, zapcore.DebugLevel), time.Minute, 1, 100)
	logger := zap.New(wlogzap.Core(sampler))

	for i := 0; i < 5; i++ {
		logger.Info("line", zap.Any("ctx", context.Background()))
	}

	if lines := strings.Count(buf.String(), "\n"); lines != 1 {
		t.Errorf("the sampler wrote %d lines, want 1", lines)
	}
}

// TestZap_C2_PluginBindsLogger proves that the plugin stores a bound logger in the event
// context, and that its entries fold.
func TestZap_C2_PluginBindsLogger(t *testing.T) {
	type ctxKey struct{}
	store := func(ctx context.Context, l *zap.Logger) context.Context {
		return context.WithValue(ctx, ctxKey{}, l)
	}
	base := zap.New(wlogzap.Core(jsonCore(&bytes.Buffer{}, zapcore.DebugLevel)))
	log, rec := wlogtest.New(t, wlog.WithPlugins(wlogzap.Plugin(base, store)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	bound, _ := ctx.Value(ctxKey{}).(*zap.Logger)
	if bound == nil {
		t.Fatal("the plugin stored no logger in the context")
	}
	bound.Info("from the app", zap.String("k", "v"))
	end()

	line := onlyLine(t, rec)
	if line["msg"] != "from the app" {
		t.Errorf("line = %v, want the message of the bound logger", line)
	}
}

// TestZap_C2_NestedObjectStaysObject proves that a field holding a map stays an object.
func TestZap_C2_NestedObjectStaysObject(t *testing.T) {
	log, rec := wlogtest.New(t)
	logger := zap.New(wlogzap.Core(jsonCore(&bytes.Buffer{}, zapcore.DebugLevel)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	logger.Info("obj", zap.Any("ctx", ctx), zap.Any("order", map[string]any{"id": "A-1"}))
	end()

	attrs, _ := onlyLine(t, rec)["attrs"].(map[string]any)
	order, _ := attrs["order"].(map[string]any)
	if order["id"] != "A-1" {
		t.Errorf("attrs.order = %v, want a nested object", attrs["order"])
	}
}

// TestZap_Bind_ReturnsFieldLogger proves that Bind adds the context to a logger.
func TestZap_Bind_ReturnsFieldLogger(t *testing.T) {
	log, rec := wlogtest.New(t)
	logger := zap.New(wlogzap.Core(jsonCore(&bytes.Buffer{}, zapcore.DebugLevel)))

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlogzap.Bind(ctx, logger).Warn("bound", zap.String("k", "v"))
	end()

	line := onlyLine(t, rec)
	if line["level"] != "warn" || line["msg"] != "bound" {
		t.Errorf("line = %v, want a warn line", line)
	}
}

// jsonCore returns a JSON core over one buffer.
func jsonCore(buf *bytes.Buffer, level zapcore.Level) zapcore.Core {
	return zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(buf), level)
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
