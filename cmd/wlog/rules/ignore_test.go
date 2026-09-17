package rules_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// TestMap_PAR30_IgnoreNeedsReason proves a directive with a reason suppresses that rule and is
// counted, and that a directive with no reason is itself a finding.
func TestMap_PAR30_IgnoreNeedsReason(t *testing.T) {
	pkgs, points := fixture(t, "ignore_app")

	ignored := rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleIgnored"), rules.Config{})
	print := checkByID(t, ignored, rules.RuleNoPrint)
	if !print.Suppressed {
		t.Errorf("the directive did not suppress the rule: %+v", print)
	}
	if print.Reason == "" {
		t.Error("the suppressed check carries no reason")
	}
	if rules.SuppressedCount(ignored) != 1 {
		t.Errorf("SuppressedCount = %d, want 1", rules.SuppressedCount(ignored))
	}

	bad := rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleBadIgnore"), rules.Config{})
	print = checkByID(t, bad, rules.RuleNoPrint)
	if print.Suppressed {
		t.Error("a directive with no reason suppressed the rule")
	}
	if print.Pass {
		t.Errorf("a directive with no reason excused the rule: %+v", print)
	}
	found := false
	for _, check := range bad {
		if check.ID == rules.RuleIgnoreReason && !check.Pass {
			found = true
		}
	}
	if !found {
		t.Errorf("a directive with no reason is not a finding: %+v", bad)
	}
}
