package rules

import (
	"go/ast"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// errorsReachWlog passes when a handler that returns a non-nil error also calls
// wlog.Error. A handler that never returns a non-nil error has nothing to report, so
// the rule passes.
func errorsReachWlog(pkg *packages.Package, point entry.Point) Check {
	if !returnsNonNilError(point.Node) {
		return pass(RuleErrors, WeightErrors)
	}
	if bodyCalls(pkg, point, func(pkgPath, name string) bool {
		return pkgPath == wlogPath && name == "Error"
	}) {
		return pass(RuleErrors, WeightErrors)
	}
	return fail(RuleErrors, WeightErrors, "handler returns a non-nil error without calling wlog.Error")
}

// returnsNonNilError reports whether the handler has a return statement with a result
// that is not the literal nil.
func returnsNonNilError(node ast.Node) bool {
	body := bodyOf(node)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			if id, ok := result.(*ast.Ident); ok && id.Name == "nil" {
				continue
			}
			found = true
		}
		return true
	})
	return found
}
