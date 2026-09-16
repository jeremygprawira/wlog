package analyzer_test

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/jeremygprawira/wlog/cmd/wlog/analyzer"
	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// TestAnalyzer_ReportsFailedRules proves the analyzer reports one diagnostic per failed
// rule, at the handler, and stays silent about the rules that pass.
func TestAnalyzer_ReportsFailedRules(t *testing.T) {
	pkgs, err := entry.Load("../testdata/rules_app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, pkg := range pkgs {
		for _, pkgErr := range pkg.Errors {
			t.Fatalf("load error: %v", pkgErr)
		}
	}
	pkg := pkgs[0]

	var messages []string
	pass := &analysis.Pass{
		Analyzer:  analyzer.Analyzer,
		Fset:      pkg.Fset,
		Files:     pkg.Syntax,
		Pkg:       pkg.Types,
		TypesInfo: pkg.TypesInfo,
		Report:    func(d analysis.Diagnostic) { messages = append(messages, d.Message) },
	}
	if _, err := analyzer.Analyzer.Run(pass); err != nil {
		t.Fatalf("Run: %v", err)
	}

	counts := map[string]int{}
	for _, message := range messages {
		id, _, _ := strings.Cut(message, ":")
		counts[id]++
	}
	want := map[string]int{
		"context.set":         1,
		"logging.no_print":    1,
		"keys.no_denylisted":  1,
		"sensitive.audit":     1,
		"middleware.coverage": 0,
		"errors.reach_wlog":   0,
	}
	for id, count := range want {
		if counts[id] != count {
			t.Errorf("%s reported %d times, want %d (all: %v)", id, counts[id], count, messages)
		}
	}
}
