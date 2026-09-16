package score_test

import (
	"reflect"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/cmd/wlog/score"
)

// check builds a Check for a test.
func check(id string, weight int, pass bool) rules.Check {
	return rules.Check{ID: id, Weight: weight, Pass: pass}
}

// TestPercent proves the score is the earned share of the applicable weight, rounded,
// and that a handler with no applicable rule scores 100.
func TestPercent(t *testing.T) {
	cases := []struct {
		name   string
		checks []rules.Check
		want   int
	}{
		{"all pass", []rules.Check{check("a", 30, true), check("b", 70, true)}, 100},
		{"none pass", []rules.Check{check("a", 30, false), check("b", 70, false)}, 0},
		{"half", []rules.Check{check("a", 50, true), check("b", 50, false)}, 50},
		{"rounding", []rules.Check{check("a", 45, true), check("b", 20, false)}, 69},
		{"nothing applies", nil, 100},
	}
	for _, tc := range cases {
		if got := score.Percent(tc.checks); got != tc.want {
			t.Errorf("%s: Percent = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestTotal proves the score combines every handler's weight before it divides, so a
// handler with no checks cannot lift the score of a failing one.
func TestTotal(t *testing.T) {
	byHandler := [][]rules.Check{
		{check("a", 30, true), check("b", 70, false)},
		{check("a", 30, true), check("b", 70, false)},
	}
	if got := score.Total(byHandler); got != 30 {
		t.Errorf("Total = %d, want 30", got)
	}
	if got := score.Total(nil); got != 100 {
		t.Errorf("Total of no handlers = %d, want 100", got)
	}
}

// TestFixes proves the top fixes rank failed rules by lost weight, then by rule id, and
// stop at the limit.
func TestFixes(t *testing.T) {
	byHandler := [][]rules.Check{
		{check("sensitive.audit", 40, false), check("logging.no_print", 10, false)},
		{check("sensitive.audit", 40, false), check("context.set", 20, false)},
	}
	fixes := score.Fixes(byHandler, 3)
	want := []score.Fix{
		{Rule: "sensitive.audit", Points: 80, Handlers: 2},
		{Rule: "context.set", Points: 20, Handlers: 1},
		{Rule: "logging.no_print", Points: 10, Handlers: 1},
	}
	if !reflect.DeepEqual(fixes, want) {
		t.Errorf("Fixes = %+v, want %+v", fixes, want)
	}

	limited := score.Fixes(byHandler, 1)
	if len(limited) != 1 || limited[0].Rule != "sensitive.audit" {
		t.Errorf("limited fixes = %+v, want only sensitive.audit", limited)
	}
}
