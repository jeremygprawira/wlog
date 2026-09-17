package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// errorsReachWlog passes when a handler that returns a non-nil error also calls
// wlog.Error. A handler that never returns a non-nil error has nothing to report, so
// the rule passes.
func errorsReachWlog(pkg *packages.Package, point entry.Point) Check {
	if !returnsNonNilError(pkg, point.Node) {
		// The handler cannot fail, so there is no error for wlog to reach. n/a keeps the
		// weight out of a handler that never had the chance to earn it (CLI-7).
		return notApplicable(RuleErrors, WeightErrors)
	}
	if bodyCalls(pkg, point, func(pkgPath, name string) bool {
		return pkgPath == wlogPath && name == "Error"
	}) {
		return pass(RuleErrors, WeightErrors)
	}
	return fail(RuleErrors, WeightErrors, "handler returns a non-nil error without calling wlog.Error")
}

// returnsNonNilError reports whether the handler has a return statement that can carry
// an error. The literal nil does not count, and neither does a framework responder call
// such as c.NoContent(200): it names the response, not an error to report.
func returnsNonNilError(pkg *packages.Package, node ast.Node) bool {
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
			if isResponderCall(pkg, result) {
				continue
			}
			found = true
		}
		return true
	})
	return found
}

// isResponderCall reports whether expr calls a method on an Echo or Gin context. Those
// calls write the response and return nil in the normal case, so they are not an error
// the handler forgot to report.
func isResponderCall(pkg *packages.Package, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := pkg.TypesInfo.Types[selector.X]
	if !ok {
		return false
	}
	switch namedTypePath(receiver.Type) {
	case entry.Echo4Path, entry.Echo5Path, entry.GinPath:
		return true
	default:
		return false
	}
}

// namedTypePath returns the package path of a named type, following one pointer.
func namedTypePath(t types.Type) string {
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	if named, ok := t.(*types.Named); ok && named.Obj().Pkg() != nil {
		return named.Obj().Pkg().Path()
	}
	return ""
}
