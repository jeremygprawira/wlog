package rules_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestRules_ErrorGuidance proves the rule fires on an error with no why and no fix, and
// stays quiet on a catalog error and on a handler with no error report.
func TestRules_ErrorGuidance(t *testing.T) {
	pkgs, points := fixture(t, "errguidance")

	bad := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleBad"), rules.Config{}), rules.RuleErrorGuidance)
	if bad.Pass {
		t.Error("handleBad passed: errors.New carries no guidance")
	}

	good := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleGood"), rules.Config{}), rules.RuleErrorGuidance)
	if !good.Pass {
		t.Errorf("handleGood failed: %s", good.Detail)
	}

	none := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleNone"), rules.Config{}), rules.RuleErrorGuidance)
	if none.Applicable {
		t.Errorf("handleNone: the rule applied to a handler that records no error: %+v", none)
	}
}
