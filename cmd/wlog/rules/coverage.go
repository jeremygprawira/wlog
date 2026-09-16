package rules

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// coverage passes when the handler's package calls a wlog HTTP middleware
// constructor. v1 checks the package, not the exact router, so one Middleware call
// covers every handler in the package.
func coverage(pkg *packages.Package, _ entry.Point) Check {
	for _, file := range pkg.Syntax {
		found := false
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isWlogMiddlewareCall(entry.Callee(pkg, call)) {
				found = true
				return false
			}
			return true
		})
		if found {
			return pass(RuleMiddleware, WeightMiddleware)
		}
	}
	return fail(RuleMiddleware, WeightMiddleware, "no wlog middleware call found in the package")
}

// isWlogMiddlewareCall reports whether obj is a wlog HTTP middleware constructor.
func isWlogMiddlewareCall(obj *types.Func) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return strings.HasPrefix(obj.Pkg().Path(), middlewarePkg) && obj.Name() == "Middleware"
}
