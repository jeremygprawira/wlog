// Package report builds the wlog.map.json document. It sorts every list and writes no
// timestamps, so two runs over the same code produce byte-identical output.
package report

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/cmd/wlog/score"
)

// Version is the map schema version. Version 2 adds the tool and rule versions, short framework
// ids, module relative paths, object top fixes, a summary, and evidence per rule result.
const Version = 2

// ToolVersion is the CLI's own version. A build sets it with -ldflags; a build from source says
// "dev", which is honest rather than a made-up release.
var ToolVersion = "dev"

// Handler is one entry point, its rules, and its score.
type Handler struct {
	Package   string        `json:"package"`
	Function  string        `json:"function"`
	File      string        `json:"file"`
	Line      int           `json:"line"`
	Framework string        `json:"framework"`
	Method    string        `json:"method"`
	Route     string        `json:"route"`
	Sensitive bool          `json:"sensitive"`
	Class     string        `json:"class"`
	Checks    []rules.Check `json:"checks"`
}

// Fix is one rule worth fixing, and what it costs.
type Fix struct {
	Rule     string `json:"rule"`
	Points   int    `json:"points"`
	Handlers int    `json:"handlers"`
	Fix      string `json:"fix"`
	Docs     string `json:"docs"`
}

// SummaryCounts is the count and score side of the document, so a reader or a bot needs no
// arithmetic.
type SummaryCounts struct {
	Handlers int `json:"handlers"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Score    int `json:"score"`
	// Projected is the score the app would reach if the listed fixes were made.
	Projected int    `json:"projected_score"`
	Grade     string `json:"grade"`
	MinScore  int    `json:"min_score"`
}

// Evidence is where one rule result was seen, as file:line.
type Evidence struct {
	Rule string `json:"rule"`
	File string `json:"file"`
	Line int    `json:"line"`
	At   string `json:"at"`
}

// Map is the whole document.
type Map struct {
	Version      int           `json:"version"`
	ToolVersion  string        `json:"tool_version"`
	RulesVersion int           `json:"rules_version"`
	Score        int           `json:"score"`
	MinScore     int           `json:"min_score"`
	Pass         bool          `json:"pass"`
	Grade        string        `json:"grade"`
	Summary      SummaryCounts `json:"summary"`
	Handlers     []Handler     `json:"handlers"`
	TopFixes     []Fix         `json:"top_fixes"`
	Evidence     []Evidence    `json:"evidence"`
}

// Build assembles the document from the entry points and their checks. gatePass says
// whether the score cleared the minimum and the baseline; the caller decides that.
func Build(points []entry.Point, checksByPoint [][]rules.Check, minScore int, gatePass bool) Map {
	handlers := make([]Handler, 0, len(points))
	for i, point := range points {
		checks := checksByPoint[i]
		if checks == nil {
			checks = []rules.Check{}
		}
		handlers = append(handlers, Handler{
			Package:   point.Package,
			Function:  point.Function,
			File:      point.File,
			Line:      point.Line,
			Framework: rules.FrameworkID(point.Framework),
			Method:    point.Method,
			Route:     point.Route,
			Sensitive: point.Sensitive,
			Class:     rules.Class(point),
			Checks:    checks,
		})
	}
	sort.Slice(handlers, func(i, j int) bool {
		a, b := handlers[i], handlers[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Function != b.Function {
			return a.Function < b.Function
		}
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.Method < b.Method
	})

	total := score.Total(points, checksByPoint)
	fixes := score.Fixes(checksByPoint, 3)
	projected := score.Project(points, checksByPoint, fixedRules(fixes))
	passed := 0
	for i := range handlers {
		if score.Percent(checksByPoint[i]) == 100 {
			passed++
		}
	}
	return Map{
		Version:      Version,
		ToolVersion:  ToolVersion,
		RulesVersion: rules.Version,
		Score:        total,
		Grade:        score.Grade(total),
		MinScore:     minScore,
		Pass:         gatePass,
		Summary: SummaryCounts{
			Handlers:  len(handlers),
			Passed:    passed,
			Failed:    len(handlers) - passed,
			Score:     total,
			Projected: projected,
			Grade:     score.Grade(total),
			MinScore:  minScore,
		},
		Handlers: handlers,
		TopFixes: topFixes(fixes),
		Evidence: evidenceOf(handlers),
	}
}

// fixedRules returns the rules a fix list would make pass.
func fixedRules(fixes []score.Fix) map[string]bool {
	out := make(map[string]bool, len(fixes))
	for _, fix := range fixes {
		out[fix.Rule] = true
	}
	return out
}

// topFixes turns the ranked failures into the object form the document wants, with the fix line
// and the docs link so a reader needs no other file.
func topFixes(fixes []score.Fix) []Fix {
	out := make([]Fix, 0, len(fixes))
	for _, fix := range fixes {
		text, docs := rules.FixFor(fix.Rule)
		out = append(out, Fix{
			Rule:     fix.Rule,
			Points:   fix.Points,
			Handlers: fix.Handlers,
			Fix:      text,
			Docs:     docs,
		})
	}
	return out
}

// evidenceOf returns one entry per rule result, so a bot can point at the code without reading
// the handler list.
func evidenceOf(handlers []Handler) []Evidence {
	out := make([]Evidence, 0)
	for _, handler := range handlers {
		for _, check := range handler.Checks {
			out = append(out, Evidence{
				Rule: check.ID,
				File: handler.File,
				Line: handler.Line,
				At:   fmt.Sprintf("%s:%d", handler.File, handler.Line),
			})
		}
	}
	return out
}

// Encode renders the document with two-space indentation and a trailing newline.
func Encode(m Map) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("report: encode: %w", err)
	}
	return append(data, '\n'), nil
}
