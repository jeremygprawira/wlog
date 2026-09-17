package rules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestSensitiveAudit proves a sensitive route fails without audit.Do and passes with
// it, at twice the normal weight.
func TestSensitiveAudit(t *testing.T) {
	pkgs, points := fixture(t, "rules_app")

	missing := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleSensitive"), rules.Config{}), rules.RuleSensitiveAudit)
	if missing.Pass {
		t.Error("handleSensitive passed without audit.Do")
	}
	if missing.Weight != 2*rules.WeightSensitiveAudit {
		t.Errorf("weight = %d, want %d", missing.Weight, 2*rules.WeightSensitiveAudit)
	}

	audited := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleSensitiveAudited"), rules.Config{}), rules.RuleSensitiveAudit)
	if !audited.Pass {
		t.Errorf("handleSensitiveAudited failed: %s", audited.Detail)
	}
}

// TestNoPrint proves print logging in a handler fails, and a clean handler passes.
func TestNoPrint(t *testing.T) {
	pkgs, points := fixture(t, "rules_app")

	bad := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handlePrint"), rules.Config{}), rules.RuleNoPrint)
	if bad.Pass {
		t.Error("handlePrint passed with fmt.Println in the body")
	}
	good := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleGood"), rules.Config{}), rules.RuleNoPrint)
	if !good.Pass {
		t.Errorf("handleGood failed: %s", good.Detail)
	}
}

// TestNoDenylisted proves a literal denied key fails, and a safe key passes.
func TestNoDenylisted(t *testing.T) {
	pkgs, points := fixture(t, "rules_app")

	bad := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleDeniedKey"), rules.Config{}), rules.RuleNoDenylisted)
	if bad.Pass {
		t.Error("handleDeniedKey passed with a password key")
	}
	good := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleGood"), rules.Config{}), rules.RuleNoDenylisted)
	if !good.Pass {
		t.Errorf("handleGood failed: %s", good.Detail)
	}
}

// TestSensitivePatterns proves a configured pattern marks a route sensitive.
func TestSensitivePatterns(t *testing.T) {
	if rules.Sensitive("/orders", nil) {
		t.Error("/orders is sensitive by default, want not")
	}
	if !rules.Sensitive("/orders", []string{"^/order"}) {
		t.Error("configured pattern ^/order did not match /orders")
	}
	if !rules.Sensitive("/v1/payments", nil) {
		t.Error("/v1/payments is not sensitive, want it to match pay")
	}
}

// TestConfig_LoadYAML proves a YAML config file sets the sensitive patterns and the
// minimum score.
func TestConfig_LoadYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wlog.map.yaml")
	body := "sensitive_routes:\n  - \"^/v1/payouts\"\nmin_score: 77\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := rules.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.SensitivePatterns) != 1 || cfg.SensitivePatterns[0] != "^/v1/payouts" {
		t.Errorf("SensitivePatterns = %v", cfg.SensitivePatterns)
	}
	if cfg.MinScore != 77 {
		t.Errorf("MinScore = %d, want 77", cfg.MinScore)
	}
}

// TestConfig_Find proves the default lookup reads only wlog.map.yaml, never the tool's own
// wlog.map.json output.
func TestConfig_Find(t *testing.T) {
	dir := t.TempDir()
	if rules.FindConfig(dir) != "" {
		t.Fatal("FindConfig found a config in an empty directory")
	}
	if err := os.WriteFile(filepath.Join(dir, "wlog.map.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := rules.FindConfig(dir); got != "" {
		t.Errorf("FindConfig = %q, want no config: only wlog.map.yaml is read", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "wlog.map.yaml"), []byte("min_score: 1\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := rules.FindConfig(dir); got != filepath.Join(dir, "wlog.map.yaml") {
		t.Errorf("FindConfig = %q, want the YAML file", got)
	}
}
