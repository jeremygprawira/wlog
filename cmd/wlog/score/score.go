// Package score turns rule checks into one deterministic 0-100 score and a ranked list
// of fixes. It is a pure function of the checks, so the same code always scores the
// same.
package score

import (
	"math"
	"sort"

	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// Percent is one handler's score: the earned share of its applicable weight, rounded.
// A handler with no applicable rule scores 100.
func Percent(checks []rules.Check) int {
	earned, applicable := 0, 0
	for _, check := range checks {
		applicable += check.Weight
		if check.Pass {
			earned += check.Weight
		}
	}
	return percent(earned, applicable)
}

// Total is the app score: every handler's weight counts once, before the division. A
// run with no handlers scores 100.
func Total(byHandler [][]rules.Check) int {
	earned, applicable := 0, 0
	for _, checks := range byHandler {
		for _, check := range checks {
			applicable += check.Weight
			if check.Pass {
				earned += check.Weight
			}
		}
	}
	return percent(earned, applicable)
}

// percent divides earned by applicable and rounds to the nearest whole number.
func percent(earned, applicable int) int {
	if applicable == 0 {
		return 100
	}
	return int(math.Round(100 * float64(earned) / float64(applicable)))
}

// Fix is one failed rule across the whole run: how much score it costs and how many
// handlers it fails.
type Fix struct {
	Rule     string
	Points   int
	Handlers int
}

// Fixes ranks the failed rules by lost points, then by rule id, and returns at most
// limit of them. A limit of 0 or less means no limit.
func Fixes(byHandler [][]rules.Check, limit int) []Fix {
	points := map[string]int{}
	handlers := map[string]int{}
	for _, checks := range byHandler {
		for _, check := range checks {
			if check.Pass || check.Weight == 0 {
				continue
			}
			points[check.ID] += check.Weight
			handlers[check.ID]++
		}
	}

	fixes := make([]Fix, 0, len(points))
	for rule, lost := range points {
		fixes = append(fixes, Fix{Rule: rule, Points: lost, Handlers: handlers[rule]})
	}
	sort.Slice(fixes, func(i, j int) bool {
		if fixes[i].Points != fixes[j].Points {
			return fixes[i].Points > fixes[j].Points
		}
		return fixes[i].Rule < fixes[j].Rule
	})
	if limit > 0 && len(fixes) > limit {
		fixes = fixes[:limit]
	}
	return fixes
}
