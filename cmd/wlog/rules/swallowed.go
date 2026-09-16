package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// swallowedError passes unless the handler discards an error and reports nothing. Two
// shapes fail the rule: an error value assigned to the blank identifier, and an
// `if err != nil` branch with an empty body and no else.
//
// The rule stays quiet on every path it cannot fully resolve. A returned error counts as
// handled, because the caller may report it.
func swallowedError(pkg *packages.Package, point entry.Point) Check {
	body := bodyOf(point.Node)
	if body == nil {
		return pass(RuleSwallowedError, WeightSwallowedError)
	}
	if findsSwallowedError(pkg, body) {
		return fail(RuleSwallowedError, WeightSwallowedError, "error is discarded with no report")
	}
	return pass(RuleSwallowedError, WeightSwallowedError)
}

// findsSwallowedError walks the body for either bad shape.
func findsSwallowedError(pkg *packages.Package, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			for i, target := range statement.Lhs {
				identifier, ok := target.(*ast.Ident)
				if !ok || identifier.Name != "_" || i >= len(statement.Rhs) {
					continue
				}
				if returnsError(pkg, statement.Rhs[i]) {
					found = true
					return false
				}
			}
		case *ast.IfStmt:
			if len(statement.Body.List) == 0 && statement.Else == nil && mentionsError(pkg, statement.Cond) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// returnsError reports whether the expression's type includes the error interface.
func returnsError(pkg *packages.Package, expr ast.Expr) bool {
	value, ok := pkg.TypesInfo.Types[expr]
	if !ok {
		return false
	}
	return typeIncludesError(value.Type)
}

// mentionsError reports whether the condition reads a value of an error type.
func mentionsError(pkg *packages.Package, cond ast.Expr) bool {
	mentions := false
	ast.Inspect(cond, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if value, ok := pkg.TypesInfo.Types[identifier]; ok && typeIncludesError(value.Type) {
			mentions = true
			return false
		}
		return true
	})
	return mentions
}

// typeIncludesError reports whether a type is the error interface, implements it, or is
// a tuple that holds one.
func typeIncludesError(t types.Type) bool {
	if tuple, ok := t.(*types.Tuple); ok {
		for i := 0; i < tuple.Len(); i++ {
			if typeIncludesError(tuple.At(i).Type()) {
				return true
			}
		}
		return false
	}
	errorInterface, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	if types.Implements(t, errorInterface) {
		return true
	}
	if _, isPointer := t.(*types.Pointer); !isPointer {
		return types.Implements(types.NewPointer(t), errorInterface)
	}
	return false
}
