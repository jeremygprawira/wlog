package rules

import (
	"go/ast"
	"go/types"

	"github.com/jeremygprawira/wlog/redact"
	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// noDenylisted passes when no literal key passed to a wlog setter is already denied by
// the default redactor. The redactor masks the value either way, so the field is only
// noise: the handler should not name it at all.
func noDenylisted(pkg *packages.Package, point entry.Point) Check {
	body := bodyOf(point.Node)
	if body == nil {
		return pass(RuleNoDenylisted, WeightNoDenylisted)
	}
	redactor := redact.Default()
	denied := declaredDeniedKey(pkg, redactor)
	if denied != "" {
		return fail(RuleNoDenylisted, WeightNoDenylisted, "denylisted key: "+denied)
	}
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := entry.Callee(pkg, call)
		if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != wlogPath {
			return true
		}
		if obj.Name() == "NewKey" {
			// A typed key is a field name too, and the redactor masks it either way, so a
			// declaration is as good a place to catch it as a write (CLI-14).
			for _, key := range newKeyNames(call) {
				if redactor.Denies(key) {
					denied = key
					return false
				}
			}
			return true
		}
		if !takesKey(obj.Name()) {
			return true
		}
		for _, key := range literalKeys(call, obj.Name()) {
			if redactor.Denies(key) {
				denied = key
				return false
			}
		}
		return true
	})
	if denied != "" {
		return fail(RuleNoDenylisted, WeightNoDenylisted, "denylisted key: "+denied)
	}
	return pass(RuleNoDenylisted, WeightNoDenylisted)
}

// declaredDeniedKey returns a typed key the package declares whose name the redactor denies, or
// "". The declaration usually sits outside the handler, so the whole package is searched.
func declaredDeniedKey(pkg *packages.Package, redactor *redact.Redactor) string {
	for _, file := range pkg.Syntax {
		denied := ""
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isWlogNewKeyCall(pkg, call) {
				return true
			}
			for _, key := range newKeyNames(call) {
				if redactor.Denies(key) {
					denied = key
					return false
				}
			}
			return true
		})
		if denied != "" {
			return denied
		}
	}
	return ""
}

// isWlogNewKeyCall reports whether a call is wlog.NewKey, however its type argument is written.
func isWlogNewKeyCall(pkg *packages.Package, call *ast.CallExpr) bool {
	obj := entry.Callee(pkg, call)
	if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == wlogPath && obj.Name() == "NewKey" {
		return true
	}
	// A generic call's callee is an index expression, which entry.Callee cannot resolve.
	expr := call.Fun
	switch index := expr.(type) {
	case *ast.IndexExpr:
		expr = index.X
	case *ast.IndexListExpr:
		expr = index.X
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	obj, _ = pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == wlogPath && obj.Name() == "NewKey"
}

// newKeyNames returns the names a wlog.NewKey call declares, if it names a literal.
func newKeyNames(call *ast.CallExpr) []string {
	if len(call.Args) != 1 {
		return nil
	}
	return literalStrings(call.Args[0])
}

// takesKey reports whether the wlog function takes a field name.
func takesKey(name string) bool {
	switch name {
	case "Set", "SetGroup", "Append":
		return true
	default:
		return false
	}
}

// literalKeys returns every literal key an argument list passes to the named function.
func literalKeys(call *ast.CallExpr, name string) []string {
	switch name {
	case "Set", "Append":
		if len(call.Args) >= 2 {
			return literalStrings(call.Args[1])
		}
	case "SetGroup":
		// SetGroup(ctx, group, key, value, key, value, ...): the keys start at index 2.
		var keys []string
		for i := 2; i < len(call.Args); i += 2 {
			keys = append(keys, literalStrings(call.Args[i])...)
		}
		return keys
	}
	return nil
}

// literalStrings reads one string literal, or the literal keys of a composite literal,
// as in SetGroup(ctx, "g", map[string]any{"password": v}).
func literalStrings(expr ast.Expr) []string {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if value := entry.StringLiteral(node); value != "" {
			return []string{value}
		}
	case *ast.CompositeLit:
		var values []string
		for _, elt := range node.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := kv.Key.(*ast.BasicLit); ok {
				if value := entry.StringLiteral(key); value != "" {
					values = append(values, value)
				}
			}
		}
		return values
	}
	return nil
}
