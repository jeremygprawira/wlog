// This file tests the setup package: the drain list, the variable aliases, the service
// identity, the report, and a third-party factory.
package setup_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
	"github.com/jeremygprawira/wlog/setup"
)

// TestSetup_PAR20_MissingCredentialDisables proves that a drain with a missing required
// variable is skipped, and that the report names the variable and never its value.
func TestSetup_PAR20_MissingCredentialDisables(t *testing.T) {
	env := setup.MapEnv{
		"WLOG_DRAINS":   "axiom,loki",
		"AXIOM_TOKEN":   "tok",
		"AXIOM_DATASET": "logs",
	}

	report := setup.Resolve(setup.WithEnv(env))
	if len(report.Drains) != 1 || report.Drains[0] != "axiom" {
		t.Errorf("drains = %v, want axiom only", report.Drains)
	}
	if !hasProblem(report, "WLOG_DRAIN_DISABLED", "LOKI_URL") {
		t.Errorf("problems = %+v, want WLOG_DRAIN_DISABLED naming LOKI_URL", report.Problems)
	}

	// The same problem reaches the Logger that FromEnv builds.
	var codes []string
	wlog.New(
		setup.FromEnv(setup.WithEnv(env)),
		wlog.WithSilent(),
		wlog.OnProblem(func(p wlog.Problem) { codes = append(codes, p.Code) }),
	)
	if !strings.Contains(strings.Join(codes, ","), "WLOG_DRAIN_DISABLED") {
		t.Errorf("reported codes = %v, want WLOG_DRAIN_DISABLED", codes)
	}
}

// TestSetup_PAR19_AliasOrder proves that a variable name wins over its alias, and that an
// alias alone still configures the drain.
func TestSetup_PAR19_AliasOrder(t *testing.T) {
	v := axiomVar(t, "AXIOM_TOKEN")

	both := setup.MapEnv{"AXIOM_TOKEN": "primary", "AXIOM_API_KEY": "fallback"}
	if got, _ := v.Value(both); got != "primary" {
		t.Errorf("value with both names = %q, want primary", got)
	}
	aliasOnly := setup.MapEnv{"AXIOM_API_KEY": "fallback"}
	if got, _ := v.Value(aliasOnly); got != "fallback" {
		t.Errorf("value with the alias alone = %q, want fallback", got)
	}

	// The built-in factory reads the alias, so the drain still builds.
	report := setup.Resolve(setup.WithEnv(setup.MapEnv{
		"WLOG_DRAINS":   "axiom",
		"AXIOM_API_KEY": "fallback",
		"AXIOM_DATASET": "logs",
	}))
	if len(report.Drains) != 1 {
		t.Errorf("drains = %v, want axiom from the alias", report.Drains)
	}
}

// TestSetup_BET16_BuildInfoService proves that with no service variables the module path
// names the service.
//
// A Go 1.21 test binary carries no build info, so the test runs only on a toolchain that
// gives one. The fallback itself reads the build info of a real binary, which carries the
// module path on every release.
func TestSetup_BET16_BuildInfoService(t *testing.T) {
	if info, ok := debug.ReadBuildInfo(); !ok || info.Main.Path == "" {
		t.Skip("this Go release reports no build info inside a test binary")
	}
	mem := memory.New(0)
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithDrains(mem),
		setup.FromEnv(setup.WithEnv(setup.MapEnv{})),
	)
	_, end := wlog.Start(log.WithContext(context.Background()), "checkout")
	end()

	events := mem.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	service, _ := events[0]["service"].(map[string]any)
	if service == nil || service["name"] == "" || service["name"] == nil {
		t.Fatalf("service = %v, want the module path as the name", events[0]["service"])
	}
	if want := modulePath(t); service["name"] != want {
		t.Errorf("service.name = %v, want the module path %q", service["name"], want)
	}
}

// TestSetup_ResolveHidesSecrets proves that the report never holds the value of a
// variable marked Secret.
func TestSetup_ResolveHidesSecrets(t *testing.T) {
	const secret = "top-secret-token"
	report := setup.Resolve(setup.WithEnv(setup.MapEnv{
		"WLOG_DRAINS":         "webhook",
		"WLOG_WEBHOOK_URL":    "http://example.invalid",
		"WLOG_WEBHOOK_SECRET": secret,
	}))

	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("the report holds the secret value: %s", body)
	}
	marked := false
	for _, v := range report.Vars {
		if v.Name == "WLOG_WEBHOOK_SECRET" && v.Secret {
			marked = true
		}
	}
	if !marked {
		t.Errorf("vars = %+v, want WLOG_WEBHOOK_SECRET marked Secret", report.Vars)
	}
}

// TestSetup_ThirdPartyFactory proves that With adds a factory that WLOG_DRAINS can name.
func TestSetup_ThirdPartyFactory(t *testing.T) {
	built := false
	fake := setup.Factory{
		Name: "fake",
		New: func(setup.Env) (wlog.Drain, error) {
			built = true
			return wlog.DrainFunc(func(context.Context, map[string]any) {}), nil
		},
	}

	report := setup.Resolve(
		setup.With(fake),
		setup.WithEnv(setup.MapEnv{"WLOG_DRAINS": "fake"}),
	)
	if !built {
		t.Error("the third-party factory never ran")
	}
	if len(report.Drains) != 1 || report.Drains[0] != "fake" {
		t.Errorf("drains = %v, want fake", report.Drains)
	}
	if len(report.Problems) != 0 {
		t.Errorf("problems = %+v, want none", report.Problems)
	}
}

// hasProblem reports whether one problem holds the code and a fragment of the message.
func hasProblem(report setup.Report, code, fragment string) bool {
	for _, p := range report.Problems {
		if p.Code == code && strings.Contains(p.Message, fragment) {
			return true
		}
	}
	return false
}

// axiomVar returns one variable of the built-in axiom factory.
func axiomVar(t *testing.T, name string) setup.Var {
	t.Helper()
	for _, f := range setup.Builtins() {
		if f.Name != "axiom" {
			continue
		}
		for _, v := range f.Vars {
			if v.Name == name {
				return v
			}
		}
	}
	t.Fatalf("axiom has no variable %s", name)
	return setup.Var{}
}

// modulePath returns the module path of the running test binary.
func modulePath(t *testing.T) string {
	t.Helper()
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("no build info")
	}
	return info.Main.Path
}

// TestSetup_EveryBuiltinBuilds proves that each built-in drain builds from its own
// variables, and that an unknown drain and an unknown preset each report.
func TestSetup_EveryBuiltinBuilds(t *testing.T) {
	for _, f := range setup.Builtins() {
		t.Run(f.Name, func(t *testing.T) {
			env := setup.MapEnv{"WLOG_DRAINS": f.Name}
			for _, v := range f.Vars {
				env[v.Name] = sampleValue(t, v.Name)
			}
			report := setup.Resolve(setup.WithEnv(env))
			if len(report.Drains) != 1 || report.Drains[0] != f.Name {
				t.Errorf("drains = %v, want %s; problems %+v", report.Drains, f.Name, report.Problems)
			}
			if len(report.Vars) == 0 {
				t.Errorf("%s: no variable was reported", f.Name)
			}
		})
	}

	unknown := setup.Resolve(setup.WithEnv(setup.MapEnv{
		"WLOG_DRAINS": "nope",
		"WLOG_OUTPUT": "nope",
	}))
	if !hasProblem(unknown, "WLOG_INVALID_CONFIG", "nope") {
		t.Errorf("problems = %+v, want WLOG_INVALID_CONFIG", unknown.Problems)
	}
}

// sampleValue returns a plausible value for one variable, so every optional branch of a
// built-in factory runs.
func sampleValue(t *testing.T, name string) string {
	t.Helper()
	switch {
	case name == "AXIOM_DATASET":
		return "logs"
	case name == "SENTRY_DSN":
		return "http://public-key@example.invalid/42"
	case name == "SENTRY_ALL_EVENTS":
		return "1"
	case name == "WLOG_FILE_MAX_AGE":
		return "1h"
	case name == "WLOG_FILE_MAX_SIZE_MB", name == "WLOG_FILE_MAX_BACKUPS",
		name == "WLOG_FILE_MAX_FILES", name == "WLOG_MEMORY_SIZE":
		return "1"
	case name == "WLOG_FILE_PATH":
		return filepath.Join(t.TempDir(), "events.ndjson")
	case name == "OTEL_EXPORTER_OTLP_HEADERS":
		return "authorization=Bearer token"
	case name == "DD_SITE", name == "DATADOG_SITE":
		return "datadoghq.com"
	case strings.Contains(name, "URL"), strings.Contains(name, "ENDPOINT"), strings.Contains(name, "HOST"):
		return "http://example.invalid"
	default:
		return "value"
	}
}
