// Package analyzer exports the wlog map rules as a go/analysis analyzer, so a team can
// run them from go vet or golangci-lint instead of the CLI.
package analyzer

import (
	"golang.org/x/tools/go/analysis"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
)

// Analyzer reports one diagnostic per failed rule at the handler's position. It never
// writes a file and never fails a build by itself.
var Analyzer = &analysis.Analyzer{
	Name: "wlogmap",
	Doc:  "reports HTTP handlers that miss a wlog observability rule",
	Run:  run,
}

// run adapts the analysis pass to the shared entry and rules packages. The pass
// already holds the syntax and type information, so no extra load is needed.
func run(pass *analysis.Pass) (any, error) {
	pkg := &packages.Package{
		PkgPath:   pass.Pkg.Path(),
		Syntax:    pass.Files,
		TypesInfo: pass.TypesInfo,
		Fset:      pass.Fset,
		Types:     pass.Pkg,
	}
	for _, point := range entry.Find([]*packages.Package{pkg}) {
		for _, check := range rules.Evaluate(entry.Program([]*packages.Package{pkg}), pkg, point, rules.Config{}) {
			if check.Pass {
				continue
			}
			pass.Report(analysis.Diagnostic{
				Pos:     point.Node.Pos(),
				Message: check.ID + ": " + check.Detail,
			})
		}
	}
	return nil, nil
}
