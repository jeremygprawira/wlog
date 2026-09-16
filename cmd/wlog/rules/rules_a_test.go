package rules_test

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// fixture loads one fixture app and returns its packages and entry points.
func fixture(t *testing.T, name string) ([]*packages.Package, []entry.Point) {
	t.Helper()
	pkgs, err := entry.Load("../testdata/" + name)
	if err != nil {
		t.Fatalf("Load %s: %v", name, err)
	}
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			t.Errorf("load error in %s: %v", pkg.PkgPath, pkgErr)
		}
	}
	return pkgs, entry.Find(pkgs)
}

// pointIn returns the entry point for one function name.
func pointIn(t *testing.T, points []entry.Point, function string) entry.Point {
	t.Helper()
	for _, point := range points {
		if point.Function == function {
			return point
		}
	}
	t.Fatalf("no entry point for %s in %+v", function, points)
	return entry.Point{}
}

// checkByID returns one check, and fails the test when it is missing.
func checkByID(t *testing.T, checks []rules.Check, id string) rules.Check {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("no check %s in %+v", id, checks)
	return rules.Check{}
}

// TestCoverage proves the middleware rule passes with a wlog middleware call and fails
// without one.
func TestCoverage(t *testing.T) {
	pkgs, points := fixture(t, "rules_app")
	for _, point := range points {
		check := checkByID(t, rules.Evaluate(pkgs[0], point, rules.Config{}), rules.RuleMiddleware)
		if !check.Pass {
			t.Errorf("%s: %s failed: %s", point.Function, rules.RuleMiddleware, check.Detail)
		}
	}

	pkgs, points = fixture(t, "nomiddleware_app")
	check := checkByID(t, rules.Evaluate(pkgs[0], points[0], rules.Config{}), rules.RuleMiddleware)
	if check.Pass {
		t.Errorf("%s passed without a middleware call", rules.RuleMiddleware)
	}
}

// TestContext proves the context rule passes when the handler sets a field and fails
// when it does not.
func TestContext(t *testing.T) {
	pkgs, points := fixture(t, "rules_app")

	good := checkByID(t, rules.Evaluate(pkgs[0], pointIn(t, points, "handleGood"), rules.Config{}), rules.RuleContext)
	if !good.Pass {
		t.Errorf("handleGood: %s failed: %s", rules.RuleContext, good.Detail)
	}
	none := checkByID(t, rules.Evaluate(pkgs[0], pointIn(t, points, "handleNoContext"), rules.Config{}), rules.RuleContext)
	if none.Pass {
		t.Errorf("handleNoContext passed %s with an empty body", rules.RuleContext)
	}
}

// TestErrors proves the error rule passes when a non-nil error return is reported
// through wlog, and fails when it is not.
func TestErrors(t *testing.T) {
	pkgs, points := fixture(t, "echo_err_app")

	ok := checkByID(t, rules.Evaluate(pkgs[0], pointIn(t, points, "handleOK"), rules.Config{}), rules.RuleErrors)
	if !ok.Pass {
		t.Errorf("handleOK: %s failed: %s", rules.RuleErrors, ok.Detail)
	}
	bad := checkByID(t, rules.Evaluate(pkgs[0], pointIn(t, points, "handleFail"), rules.Config{}), rules.RuleErrors)
	if bad.Pass {
		t.Errorf("handleFail passed %s while returning an unreported error", rules.RuleErrors)
	}
}

// TestRuleOrder proves the checks come back in the fixed rule order.
func TestRuleOrder(t *testing.T) {
	pkgs, points := fixture(t, "rules_app")

	checks := rules.Evaluate(pkgs[0], pointIn(t, points, "handleGood"), rules.Config{})
	want := []string{rules.RuleMiddleware, rules.RuleContext, rules.RuleErrors, rules.RuleNoPrint, rules.RuleNoDenylisted}
	if len(checks) != len(want) {
		t.Fatalf("checks = %+v, want %v", checks, want)
	}
	for i, id := range want {
		if checks[i].ID != id {
			t.Errorf("check %d = %s, want %s", i, checks[i].ID, id)
		}
	}
}
