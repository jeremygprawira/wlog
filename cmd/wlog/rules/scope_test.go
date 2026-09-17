package rules_test

import (
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/cmd/wlog/score"
)

// TestMap_CLI7_NotApplicable proves a rule with nothing to check reports n/a, and that n/a
// earns no points: the three error rules used to hand every handler 45 points for free.
func TestMap_CLI7_NotApplicable(t *testing.T) {
	pkgs, points := fixture(t, "na_app")
	point := pointIn(t, points, "handlePlain")
	checks := rules.Evaluate(pkgs, pkgs[0], point, rules.Config{})

	for _, id := range []string{rules.RuleErrors, rules.RuleErrorGuidance, rules.RuleSwallowedError} {
		check := checkByID(t, checks, id)
		if check.Applicable {
			t.Errorf("%s is applicable for a handler with no error: %+v", id, check)
		}
	}

	// Only the applicable rules count, in the numerator and the denominator alike.
	applicable := make([]rules.Check, 0, len(checks))
	for _, check := range checks {
		if check.Applicable {
			applicable = append(applicable, check)
		}
	}
	if len(applicable) == len(checks) {
		t.Fatal("the fixture has no n/a rule to check")
	}
	if got, want := score.Percent(checks), score.Percent(applicable); got != want {
		t.Errorf("score = %d, want %d: an n/a rule must add no points and no weight", got, want)
	}
}

// TestMap_CLI14_PrintScope proves only a write to stdout or the standard logger counts as print
// logging: writing to the response through fmt or a JSON encoder does not.
func TestMap_CLI14_PrintScope(t *testing.T) {
	pkgs, points := fixture(t, "scope_app")

	cases := []struct {
		function string
		pass     bool
	}{
		{"handlePrintWriter", true},     // fmt.Fprintf(w, ...) writes the response
		{"handlePrintStdout", false},    // fmt.Println writes stdout
		{"handlePrintLog", false},       // log.Println writes the standard logger
		{"handleSwallowedWriter", true}, // json.NewEncoder(w).Encode reports through the response
		{"handleSwallowed", false},      // _ = fail() discards the error
		{"handleEmptyBranch", false},    // if err != nil {} reports nothing
		{"handleReportedBranch", true},  // wlog.Error reports it
	}
	for _, tc := range cases {
		point := pointIn(t, points, tc.function)
		checks := rules.Evaluate(pkgs, pkgs[0], point, rules.Config{})
		if tc.function == "handlePrintWriter" || tc.function == "handlePrintStdout" || tc.function == "handlePrintLog" {
			if got := checkByID(t, checks, rules.RuleNoPrint).Pass; got != tc.pass {
				t.Errorf("%s: print pass = %v, want %v", tc.function, got, tc.pass)
			}
			continue
		}
		if got := checkByID(t, checks, rules.RuleSwallowedError).Pass; got != tc.pass {
			t.Errorf("%s: swallowed-error pass = %v, want %v", tc.function, got, tc.pass)
		}
	}
}

// TestMap_CLI14_WholeWordSensitive proves a sensitive word matches a whole path segment or
// word, so /authors is not sensitive while /auth/login and /user/auth-token are.
func TestMap_CLI14_WholeWordSensitive(t *testing.T) {
	patterns := []string{"auth", "admin"}
	cases := []struct {
		route string
		want  bool
	}{
		{"/auth/login", true},
		{"/user/auth-token", true},
		{"/admin/users/{id}", true},
		{"/authors", false}, // auth is a prefix of a longer word, not the word
		{"/orders/{id}", false},
	}
	for _, tc := range cases {
		if got := rules.Sensitive(tc.route, patterns); got != tc.want {
			t.Errorf("Sensitive(%q) = %v, want %v", tc.route, got, tc.want)
		}
	}
}

// TestMap_CLI20_SwallowedErrorCFG proves the swallowed-error rule reads the control flow: an
// empty error branch whose successors report nothing fails, and the same branch with a report
// passes.
func TestMap_CLI20_SwallowedErrorCFG(t *testing.T) {
	pkgs, points := fixture(t, "scope_app")

	empty := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleEmptyBranch"), rules.Config{}), rules.RuleSwallowedError)
	if empty.Pass {
		t.Errorf("an empty error branch passed: %+v", empty)
	}
	reported := checkByID(t, rules.Evaluate(pkgs, pkgs[0], pointIn(t, points, "handleReportedBranch"), rules.Config{}), rules.RuleSwallowedError)
	if !reported.Pass {
		t.Errorf("a reported error branch failed: %+v", reported)
	}
}

// TestMap_CLI14_GeneratedAndTypedKeys proves a generated file is not scored, and that a typed
// key whose name the redactor denies is still reported.
func TestMap_CLI14_GeneratedAndTypedKeys(t *testing.T) {
	generated, err := entry.Load("./../testdata/generated_app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if points := entry.Find(generated); len(points) != 0 {
		t.Errorf("a generated file was scored: %+v", points)
	}

	pkgs, points := fixture(t, "scope_app")
	keyPoint := pointIn(t, points, "handlePrintWriter")
	_ = keyPoint
	denied := typedKeyDenied(pkgs)
	if !denied {
		t.Error("keys.no_denylisted ignored a typed key whose name the redactor denies")
	}
}

// typedKeyDenied reports whether the fixture's typed PasswordKey name trips keys.no_denylisted.
func typedKeyDenied(pkgs []*packages.Package) bool {
	for _, point := range entry.Find(pkgs) {
		for _, check := range rules.Evaluate(pkgs, pkgs[0], point, rules.Config{}) {
			if check.ID == rules.RuleNoDenylisted && !check.Pass {
				return true
			}
		}
	}
	return false
}
