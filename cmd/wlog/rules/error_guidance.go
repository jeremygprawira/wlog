package rules

import (
	"go/ast"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// catalogPath is the catalog package the rule recognizes without importing it.
const catalogPath = "github.com/jeremygprawira/wlog/catalog"

// errorGuidance passes when every error the handler reports carries repair guidance.
// A catalog error does. A plain errors.New or an Errorf call does not. A handler that
// reports nothing passes, because there is nothing to guide.
//
// The rule is static: it reads how the error value was built, never a runtime extractor.
func errorGuidance(pkg *packages.Package, point entry.Point) Check {
	body := bodyOf(point.Node)
	if body == nil {
		return pass(RuleErrorGuidance, WeightErrorGuidance)
	}
	if !recordsError(pkg, body) {
		// With nothing recorded there is nothing to guide, so the rule does not apply.
		return notApplicable(RuleErrorGuidance, WeightErrorGuidance)
	}
	guided := true
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := entry.Callee(pkg, call)
		if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != wlogPath {
			return true
		}
		switch obj.Name() {
		case "Error":
			if len(call.Args) < 2 || !carriesGuidance(pkg, body, call.Args[1]) {
				guided = false
				return false
			}
		case "Errorf":
			guided = false
			return false
		}
		return true
	})
	if guided {
		return pass(RuleErrorGuidance, WeightErrorGuidance)
	}
	return fail(RuleErrorGuidance, WeightErrorGuidance, "error reaches the event with no why and no fix")
}

// recordsError reports whether the handler reports an error through wlog at all.
func recordsError(pkg *packages.Package, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := entry.Callee(pkg, call)
		if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == wlogPath && obj.Name() == "Error" {
			found = true
			return false
		}
		return true
	})
	return found
}

// carriesGuidance reports whether the error expression shows repair guidance. An expression the
// rule cannot resolve counts as guided, so it stays quiet: a value it cannot follow, such as the
// result of a call, is not evidence of a missing fix (CLI-7).
func carriesGuidance(pkg *packages.Package, body *ast.BlockStmt, expr ast.Expr) bool {
	switch value := expr.(type) {
	case *ast.CallExpr:
		return isCatalogError(pkg, value)
	case *ast.Ident:
		return assignedFromCatalog(pkg, body, value.Name)
	default:
		return true
	}
}

// isCatalogError reports whether the call is a catalog registry's Err method.
func isCatalogError(pkg *packages.Package, call *ast.CallExpr) bool {
	obj := entry.Callee(pkg, call)
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == catalogPath && obj.Name() == "Err"
}

// assignedFromCatalog reports whether name is assigned from a catalog error inside body.
func assignedFromCatalog(pkg *packages.Package, body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, target := range assign.Lhs {
			identifier, ok := target.(*ast.Ident)
			if !ok || identifier.Name != name || i >= len(assign.Rhs) {
				continue
			}
			if call, ok := assign.Rhs[i].(*ast.CallExpr); ok && isCatalogError(pkg, call) {
				found = true
			}
		}
		return true
	})
	return found
}
