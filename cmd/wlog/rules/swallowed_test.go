package rules_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestRules_SwallowedError proves the rule fires on a discarded error and an empty error
// branch, and stays quiet on a reported error and on a returned one.
func TestRules_SwallowedError(t *testing.T) {
	pkgs, points := fixture(t, "swallowed")

	discarded := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleDiscard"), rules.Config{}), rules.RuleSwallowedError)
	if discarded.Pass {
		t.Error("handleDiscard passed: the error is discarded with no report")
	}

	empty := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleEmptyBranch"), rules.Config{}), rules.RuleSwallowedError)
	if empty.Pass {
		t.Error("handleEmptyBranch passed: the error branch does nothing")
	}

	good := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleGood"), rules.Config{}), rules.RuleSwallowedError)
	if !good.Pass {
		t.Errorf("handleGood failed: %s", good.Detail)
	}

	unresolvable := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleUnresolvable"), rules.Config{}), rules.RuleSwallowedError)
	if !unresolvable.Pass {
		t.Errorf("handleUnresolvable failed: %s", unresolvable.Detail)
	}
}
