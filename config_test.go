package wlog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func TestCore_Env_ServiceFromEnvVars(t *testing.T) {
	t.Setenv("WLOG_SERVICE", "go-customer")
	t.Setenv("WLOG_VERSION", "2.0.0")
	t.Setenv("WLOG_ENV", "staging")

	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
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
		_, end := wlog.Start(ctx, "op")
		end()
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
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
		_, end := wlog.Start(ctx, "op") // default level info, below WLOG_LEVEL=warn
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
		_, end := wlog.Start(ctx, "op")
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
		_, end := wlog.Start(ctx, "op")
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
		_, end := wlog.Start(ctx, "op")
		end()
	})
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("invalid WLOG_FORMAT did not fall back to the default (JSON, no env set): %q", out)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "WLOG_FORMAT") {
		t.Errorf("errs = %v, want one entry naming WLOG_FORMAT", errs)
	}
}

// TestCore_CORE29_ServiceKeepsEnv proves that an empty WithService argument keeps
// the value the environment gave, so a caller may pass only what it knows.
func TestCore_CORE29_ServiceKeepsEnv(t *testing.T) {
	t.Setenv("WLOG_SERVICE", "from-env")
	t.Setenv("WLOG_VERSION", "9.9.9")
	t.Setenv("WLOG_ENV", "production")

	log, rec := wlogtest.New(t, wlog.WithService("", "", "local"))
	ctx := log.WithContext(context.Background())
	_, end := wlog.Start(ctx, "op")
	end()

	svc, _ := rec.Last()["service"].(map[string]any)
	if svc["name"] != "from-env" || svc["version"] != "9.9.9" {
		t.Errorf("service = %v, want the env name and version", svc)
	}
	if svc["env"] != "local" {
		t.Errorf("service.env = %v, want the explicit local", svc["env"])
	}
}

// TestCore_CORE30_InvalidLevelRejected proves that a level outside the four is
// rejected: the option keeps the current level, SetLevel keeps the event's level,
// and both report through OnError.
func TestCore_CORE30_InvalidLevelRejected(t *testing.T) {
	var reported int
	var mu sync.Mutex

	log, rec := wlogtest.New(t,
		wlog.WithLevel(wlog.Level("verbose")),
		wlog.OnError(func(error, string) {
			mu.Lock()
			defer mu.Unlock()
			reported++
		}),
	)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "op")
	wlog.SetLevel(ctx, wlog.Level("loud"))
	wlog.Info(ctx, "still here")
	end()

	if got := rec.Last()["level"]; got != "info" {
		t.Errorf("level = %v, want info (the invalid level was accepted)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if reported < 2 {
		t.Errorf("OnError heard %d reports, want one for each rejected level", reported)
	}
}

// keepingPlugin is a Plugin that also keeps events, so the test can prove that a
// plugin's Keeper and WithSampler combine with OR.
type keepingPlugin struct{ keep bool }

// Name names the plugin.
func (keepingPlugin) Name() string { return "keeper" }

// Keep returns the configured answer.
func (p keepingPlugin) Keep(context.Context, map[string]any) bool { return p.keep }

// TestCore_CORE31_KeepersCombineOr proves that every Keeper runs and that one
// keeper which says yes keeps the event.
func TestCore_CORE31_KeepersCombineOr(t *testing.T) {
	for _, tc := range []struct {
		name          string
		sampler, plug bool
		want          int
	}{
		{"the plugin keeps", false, true, 1},
		{"the sampler keeps", true, false, 1},
		{"neither keeps", false, false, 0},
		{"both keep", true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t,
				wlog.WithSampler(wlog.KeeperFunc(func(context.Context, map[string]any) bool { return tc.sampler })),
				wlog.WithPlugins(keepingPlugin{keep: tc.plug}),
			)
			ctx := log.WithContext(context.Background())
			_, end := wlog.Start(ctx, "op")
			end()

			if rec.Count() != tc.want {
				t.Errorf("recorded %d events, want %d", rec.Count(), tc.want)
			}
		})
	}
}

// TestCore_CORE31_PluginsCopy proves that Plugins returns a copy, so a caller
// cannot change the logger's list.
func TestCore_CORE31_PluginsCopy(t *testing.T) {
	log := wlog.New(wlog.WithPlugins(keepingPlugin{}))
	first := log.Plugins()
	if len(first) != 1 {
		t.Fatalf("Plugins = %v, want one plugin", first)
	}
	first[0] = nil
	if second := log.Plugins(); second[0] == nil {
		t.Error("Plugins returned the logger's own slice")
	}
}
