package rules

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// coverage passes when anywhere in the program calls a wlog HTTP middleware constructor.
//
// The search covers every loaded package rather than the handler's own one, because the common
// layout wraps the router in main and builds it in an internal package: a rule that looked only
// at the handler's package failed every handler there. It still checks the program, not the
// exact router, so one Middleware call covers the handlers it wraps.
func coverage(program []*packages.Package, pkg *packages.Package, _ entry.Point) Check {
	candidates := program
	if len(candidates) == 0 {
		candidates = []*packages.Package{pkg}
	}
	for _, candidate := range candidates {
		for _, file := range candidate.Syntax {
			found := false
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if isWlogMiddlewareCall(entry.Callee(candidate, call)) {
					found = true
					return false
				}
				return true
			})
			if found {
				return pass(RuleMiddleware, WeightMiddleware)
			}
		}
	}
	return fail(RuleMiddleware, WeightMiddleware, "no wlog middleware call found in the program")
}

// isWlogMiddlewareCall reports whether obj is a wlog HTTP middleware constructor.
func isWlogMiddlewareCall(obj *types.Func) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return strings.HasPrefix(obj.Pkg().Path(), middlewarePkg) && obj.Name() == "Middleware"
}
