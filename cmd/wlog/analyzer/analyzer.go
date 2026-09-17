// Package analyzer exports the wlog map rules as a go/analysis analyzer, so a team can run them
// from go vet or golangci-lint instead of the CLI.
package analyzer

import (
	"go/ast"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// options holds the analyzer's flags, one set per process, the way go/analysis expects them.
type options struct {
	suggest bool
	rules   string
}

// opts is the single flag set. go/analysis registers flags on the analyzer and calls Run with
// their values already parsed.
var opts options

// Analyzer reports one diagnostic per failed rule at the code that fails it. It never writes a
// file and never fails a build by itself.
var Analyzer = &analysis.Analyzer{
	Name: "wlogmap",
	Doc:  "reports HTTP handlers that miss a wlog observability rule",
	Run:  run,
}

func init() {
	Analyzer.Flags.BoolVar(&opts.suggest, "suggest", false, "also report suggestions, which never change the score")
	Analyzer.Flags.StringVar(&opts.rules, "rules", "", "comma-separated rule ids to run; empty runs every rule")
}

// run adapts the analysis pass to the shared entry and rules packages. The pass already holds the
// syntax and type information, so no extra load is needed.
func run(pass *analysis.Pass) (any, error) {
	pkg := packageFrom(pass)
	// A test file is not the app's code to grade, so its handlers are left out.
	files := make([]*ast.File, 0, len(pass.Files))
	for _, file := range pass.Files {
		if strings.HasSuffix(filepath.Base(pass.Fset.Position(file.Pos()).Filename), "_test.go") {
			continue
		}
		files = append(files, file)
	}
	pkg.Syntax = files

	cfg := config(pass)
	selected := selectedRules(opts.rules)

	for _, point := range entry.Find([]*packages.Package{pkg}) {
		for _, check := range rules.Evaluate(entry.Program([]*packages.Package{pkg}), pkg, point, cfg) {
			if !check.Applicable || check.Pass {
				// A passing rule and a rule with nothing to check are both silent.
				continue
			}
			if check.Suggestion && !opts.suggest {
				// A suggestion is advice, not a failure, and it costs nothing unless a caller
				// asks for it.
				continue
			}
			if len(selected) > 0 && !selected[check.ID] {
				// -rules was given, so a rule outside the list stays quiet.
				continue
			}
			at := point.Node
			if check.Node != nil {
				// A rule that found the offending call reports there, so an editor jumps to the
				// line to change rather than to the handler that holds it.
				at = check.Node
			}
			pass.Report(analysis.Diagnostic{
				Pos:     at.Pos(),
				Message: check.ID + ": " + check.Detail,
			})
		}
	}
	return nil, nil
}

// packageFrom builds the packages.Package the shared rules read from one analysis pass.
func packageFrom(pass *analysis.Pass) *packages.Package {
	return &packages.Package{
		PkgPath:   pass.Pkg.Path(),
		Syntax:    pass.Files,
		TypesInfo: pass.TypesInfo,
		Fset:      pass.Fset,
		Types:     pass.Pkg,
	}
}

// selectedRules reads the -rules list. An empty list selects every rule.
func selectedRules(list string) map[string]bool {
	selected := map[string]bool{}
	for _, id := range strings.Split(list, ",") {
		if id = strings.TrimSpace(id); id != "" {
			selected[id] = true
		}
	}
	return selected
}

// config reads wlog.map.yaml from the package's directory, so the analyzer honors the same
// sensitive routes the CLI does. A missing or unreadable file is not a load failure: the rule
// set works without it.
func config(pass *analysis.Pass) rules.Config {
	dir := "."
	if len(pass.Files) > 0 {
		dir = filepath.Dir(pass.Fset.Position(pass.Files[0].Pos()).Filename)
	}
	path := rules.FindConfig(dir)
	if path == "" {
		return rules.Config{}
	}
	cfg, err := rules.LoadConfig(path)
	if err != nil {
		// A linter that stopped work because a config file was malformed would be worse than one
		// that keeps its defaults: the caller sees the defaults and can fix the file.
		return rules.Config{}
	}
	return cfg
}
