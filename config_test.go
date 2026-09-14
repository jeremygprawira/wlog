package wlog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
)

func TestCore_Env_ServiceFromEnvVars(t *testing.T) {
	t.Setenv("WLOG_SERVICE", "go-customer")
	t.Setenv("WLOG_VERSION", "2.0.0")
	t.Setenv("WLOG_ENV", "staging")

	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	svc := got["service"].(map[string]any)
	if svc["name"] != "go-customer" || svc["version"] != "2.0.0" || svc["env"] != "staging" {
		t.Errorf("service = %v", svc)
	}
}

func TestCore_Env_ExplicitOptionWinsOverEnv(t *testing.T) {
	t.Setenv("WLOG_SERVICE", "from-env")
	log := wlog.New(wlog.WithService("from-option", "1.0.0", "prod"), wlog.WithFormat(wlog.FormatJSON))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	svc := got["service"].(map[string]any)
	if svc["name"] != "from-option" {
		t.Errorf("service.name = %v, want from-option (explicit option must win)", svc["name"])
	}
}

func TestCore_Env_Level(t *testing.T) {
	t.Setenv("WLOG_LEVEL", "warn")
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op") // default level info, below WLOG_LEVEL=warn
		end()
	})
	if out != "" {
		t.Errorf("WLOG_LEVEL=warn did not filter an info-level event: %q", out)
	}
}

func TestCore_Env_Format(t *testing.T) {
	t.Setenv("WLOG_FORMAT", "pretty")
	log := wlog.New()
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		end()
	})
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("WLOG_FORMAT=pretty produced JSON: %q", out)
	}
}

func TestCore_Env_InvalidLevel_ReportsAndUsesDefault(t *testing.T) {
	t.Setenv("WLOG_LEVEL", "not-a-level")
	var errs []string
	log := wlog.New(
		wlog.WithFormat(wlog.FormatJSON),
		wlog.OnError(func(err error, source string) { errs = append(errs, fmt.Sprintf("%s: %v", source, err)) }),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		end()
	})
	if out == "" {
		t.Error("invalid WLOG_LEVEL caused every event to be dropped instead of falling back to the default")
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "WLOG_LEVEL") {
		t.Errorf("errs = %v, want one entry naming WLOG_LEVEL", errs)
	}
}

func TestCore_Env_InvalidFormat_ReportsAndUsesDefault(t *testing.T) {
	t.Setenv("WLOG_FORMAT", "not-a-format")
	var errs []string
	log := wlog.New(wlog.OnError(func(err error, source string) {
		errs = append(errs, fmt.Sprintf("%s: %v", source, err))
	}))

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		end()
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("invalid WLOG_FORMAT did not fall back to the default (JSON, no env set): %q", out)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "WLOG_FORMAT") {
		t.Errorf("errs = %v, want one entry naming WLOG_FORMAT", errs)
	}
}
