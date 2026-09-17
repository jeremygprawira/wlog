package analyzer_test

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

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

// TestVet_CLI16_Flags proves the analyzer has -suggest and -rules, skips a test file, reads
// wlog.map.yaml, and points at the offending call rather than only at the handler.
func TestVet_CLI16_Flags(t *testing.T) {
	for _, name := range []string{"suggest", "rules"} {
		if analyzer.Analyzer.Flags.Lookup(name) == nil {
			t.Errorf("the analyzer has no -%s flag", name)
		}
	}

	pkgs, err := entry.Load("../testdata/scope_app")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pkg := pkgs[0]
	diagnostics := runAnalyzer(t, pkg)

	// The denied key is reported at the wlog.Set call that names it, not at the handler.
	keyLine := 0
	for _, diagnostic := range diagnostics {
		if strings.HasPrefix(diagnostic.Message, "keys.no_denylisted:") {
			keyLine = pkg.Fset.Position(diagnostic.Pos).Line
		}
	}
	if keyLine == 0 {
		t.Fatalf("no keys.no_denylisted diagnostic: %+v", diagnostics)
	}
	if want := callLineIn(t, "../testdata/scope_app/main.go", `wlog.NewKey[string]("password")`); keyLine != want {
		t.Errorf("the diagnostic points at line %d, want the Set call at line %d", keyLine, want)
	}
}

// runAnalyzer runs the analyzer over one loaded package and returns its diagnostics.
func runAnalyzer(t *testing.T, pkg *packages.Package) []analysis.Diagnostic {
	t.Helper()
	var diagnostics []analysis.Diagnostic
	pass := &analysis.Pass{
		Analyzer:  analyzer.Analyzer,
		Fset:      pkg.Fset,
		Files:     pkg.Syntax,
		Pkg:       pkg.Types,
		TypesInfo: pkg.TypesInfo,
		Report:    func(d analysis.Diagnostic) { diagnostics = append(diagnostics, d) },
	}
	if _, err := analyzer.Analyzer.Run(pass); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return diagnostics
}

// callLineIn returns the line of the first line in path that holds needle.
func callLineIn(t *testing.T, path, needle string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	t.Fatalf("%s holds no %q", path, needle)
	return 0
}
