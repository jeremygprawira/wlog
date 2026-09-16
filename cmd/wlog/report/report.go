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

// Version is the map schema version.
const Version = 1

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

// Map is the whole document.
type Map struct {
	Version  int       `json:"version"`
	Score    int       `json:"score"`
	MinScore int       `json:"min_score"`
	Pass     bool      `json:"pass"`
	Grade    string    `json:"grade"`
	Handlers []Handler `json:"handlers"`
	TopFixes []string  `json:"top_fixes"`
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
			Framework: point.Framework,
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
	return Map{
		Version:  Version,
		Score:    total,
		Grade:    score.Grade(total),
		MinScore: minScore,
		Pass:     gatePass,
		Handlers: handlers,
		TopFixes: fixLines(score.Fixes(checksByPoint, 3)),
	}
}

// fixLines formats the top fixes as one line each.
func fixLines(fixes []score.Fix) []string {
	lines := make([]string, 0, len(fixes))
	for _, fix := range fixes {
		handlerWord := "handlers"
		if fix.Handlers == 1 {
			handlerWord = "handler"
		}
		lines = append(lines, fmt.Sprintf("%s: %d points lost across %d %s", fix.Rule, fix.Points, fix.Handlers, handlerWord))
	}
	return lines
}

// Encode renders the document with two-space indentation and a trailing newline.
func Encode(m Map) ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("report: encode: %w", err)
	}
	return append(data, '\n'), nil
}
