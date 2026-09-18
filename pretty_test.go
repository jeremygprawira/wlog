package wlog_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
)

func TestCore_Format_DefaultEnv_IsJSON(t *testing.T) {
	log := wlog.New()
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected JSON output, got %q", out)
	}
}

func TestCore_Format_DevEnv_IsPretty(t *testing.T) {
	log := wlog.New(wlog.WithService("svc", "1.0.0", "dev"))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "order.create")
		end()

		flushWriter(t, log)
	})
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected pretty output in dev, got JSON: %q", out)
	}
	if !strings.Contains(out, "order.create") {
		t.Errorf("pretty output missing operation name: %q", out)
	}
	if !strings.Contains(strings.ToUpper(out), "INFO") {
		t.Errorf("pretty output missing level: %q", out)
	}
}

func TestCore_Format_ExplicitOverridesEnv(t *testing.T) {
	log := wlog.New(wlog.WithService("svc", "1.0.0", "dev"), wlog.WithFormat(wlog.FormatJSON))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("WithFormat(FormatJSON) did not override the dev-env default: %q", out)
	}

	log2 := wlog.New(wlog.WithFormat(wlog.FormatPretty))
	out2 := captureStdout(t, func() {
		ctx := log2.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
		flushWriter(t, log2)
	})
	if strings.HasPrefix(strings.TrimSpace(out2), "{") {
		t.Errorf("WithFormat(FormatPretty) did not override the default JSON format: %q", out2)
	}
}

func TestCore_Pretty_IsRedacted(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatPretty))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "login")
		wlog.Set(ctx, "password", "hunter2")
		end()

		flushWriter(t, log)
	})
	if strings.Contains(out, "hunter2") {
		t.Errorf("pretty output leaked an unredacted secret: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("pretty output missing the redaction marker: %q", out)
	}
}

func TestCore_Pretty_ErrorBlock(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatPretty), wlog.WithErrorExtractor(customExtractor{}))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "order.get")
		wlog.Error(ctx, errAny{})
		end()

		flushWriter(t, log)
	})
	if !strings.Contains(out, "no such order id") || !strings.Contains(out, "check the order id") {
		t.Errorf("pretty output missing why/fix: %q", out)
	}
}

type errAny struct{}

func (errAny) Error() string { return "no row" }

func TestCore_Pretty_NoColorRespected(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatPretty))

	t.Setenv("NO_COLOR", "1")
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if strings.Contains(out, "\033[") {
		t.Errorf("NO_COLOR=1 but output still has ANSI escapes: %q", out)
	}

	t.Setenv("NO_COLOR", "")
	out2 := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
	})
	if !strings.Contains(out2, "\033[") {
		t.Errorf("NO_COLOR unset but output has no ANSI escapes: %q", out2)
	}
}
