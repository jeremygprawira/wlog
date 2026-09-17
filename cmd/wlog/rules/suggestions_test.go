package rules_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestRules_UseCatalog proves the suggestion fires on a literal a registry holds, and
// stays quiet on a literal no registry holds.
func TestRules_UseCatalog(t *testing.T) {
	pkgs, points := fixture(t, "suggestions")

	fired := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleLiteral"), rules.Config{}), rules.RuleUseCatalog)
	if fired.Pass {
		t.Error("handleLiteral passed: APP_NOT_FOUND is in the registry")
	}

	other := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleOtherLiteral"), rules.Config{}), rules.RuleUseCatalog)
	if !other.Pass {
		t.Errorf("handleOtherLiteral failed: %s", other.Detail)
	}

	// The same literal with no registry stays quiet.
	quietPkgs, quietPoints := fixture(t, "nocatalog")
	quiet := checkByID(t, rules.Evaluate(quietPkgs, quietPkgs[0], quietPoints[0], rules.Config{}), rules.RuleUseCatalog)
	if !quiet.Pass {
		t.Errorf("no-registry fixture failed: %s", quiet.Detail)
	}
}

// TestRules_AuditCoverage proves the suggestion fires on a write route with no audit,
// and stays quiet on a read route.
func TestRules_AuditCoverage(t *testing.T) {
	pkgs, points := fixture(t, "suggestions")

	write := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleWriteNoAudit"), rules.Config{}), rules.RuleAuditCoverage)
	if write.Pass {
		t.Error("handleWriteNoAudit passed: a write route with no audit record")
	}

	read := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleReadNoAudit"), rules.Config{}), rules.RuleAuditCoverage)
	if !read.Pass {
		t.Errorf("handleReadNoAudit failed: %s", read.Detail)
	}
}
