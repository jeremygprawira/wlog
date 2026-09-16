package score

import (
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// failingHandler returns one handler with a single failed check of the given weight.
func failingHandler(weight int) []rules.Check {
	return []rules.Check{{ID: "middleware.coverage", Weight: weight, Pass: false}}
}

func passingHandler(weight int) []rules.Check {
	return []rules.Check{{ID: "middleware.coverage", Weight: weight, Pass: true}}
}

// TestScore_Class proves the classes come from the method and the route.
func TestScore_Class(t *testing.T) {
	cases := []struct {
		point entry.Point
		want  string
	}{
		{entry.Point{Route: "/health"}, "read"},
		{entry.Point{Method: "POST", Route: "/orders"}, "write"},
		{entry.Point{Route: "/orders/refund"}, "sensitive"},
		{entry.Point{Method: "GET", Route: "/auth/login"}, "sensitive"},
	}
	for _, tc := range cases {
		if got := rules.Class(tc.point); got != tc.want {
			t.Errorf("Class(%+v) = %q, want %q", tc.point, got, tc.want)
		}
	}
}

// TestScore_SensitiveWeighsMore proves the same miss costs a sensitive entry more than a
// read entry.
func TestScore_SensitiveWeighsMore(t *testing.T) {
	read := []entry.Point{{Route: "/health"}}
	sensitive := []entry.Point{{Route: "/orders/refund"}}

	readScore := Total(read, [][]rules.Check{failingHandler(30)})
	sensitiveScore := Total(sensitive, [][]rules.Check{failingHandler(30)})
	if sensitiveScore >= readScore {
		t.Errorf("sensitive score %d >= read score %d, want the sensitive entry to weigh more", sensitiveScore, readScore)
	}
}

// TestScore_WeightedMix proves a passing entry lifts the total and a failing one lowers
// it, with each entry's class weight applied.
func TestScore_WeightedMix(t *testing.T) {
	points := []entry.Point{{Route: "/health"}, {Method: "POST", Route: "/orders"}}
	byHandler := [][]rules.Check{passingHandler(40), failingHandler(40)}
	// read (weight 1) passes, write (weight 1.5) fails: 1*40 / (1*40 + 1.5*40) = 40%.
	if got := Total(points, byHandler); got != 40 {
		t.Errorf("Total = %d, want 40", got)
	}
}

// TestScore_Grade proves the grade boundaries.
func TestScore_Grade(t *testing.T) {
	cases := map[int]string{100: "A", 90: "A", 89: "B", 80: "B", 79: "C", 70: "C", 69: "D", 60: "D", 59: "F", 0: "F"}
	for percent, want := range cases {
		if got := Grade(percent); got != want {
			t.Errorf("Grade(%d) = %q, want %q", percent, got, want)
		}
	}
}
