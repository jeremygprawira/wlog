package rules

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// keysStrict passes when a literal key passed to a wlog setter is either the exact name of a
// declared typed key or unrelated to every one of them.
//
// It fails on a near miss, such as Set(ctx, "orderID", v) beside NewKey[string]("order_id"):
// the typed key is declared to keep one spelling, the literal write skips the check the key
// exists for, and the event ends up with two names for one field, so a query for one of them
// misses half the rows.
func keysStrict(pkg *packages.Package, point entry.Point) Check {
	body := bodyOf(point.Node)
	if body == nil {
		return pass(RuleKeysStrict, WeightKeysStrict)
	}
	declared := declaredKeys(pkg)
	if len(declared) == 0 {
		return pass(RuleKeysStrict, WeightKeysStrict)
	}

	nearMiss := ""
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := entry.Callee(pkg, call)
		if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != wlogPath || !takesKey(obj.Name()) {
			return true
		}
		for _, key := range literalKeys(call, obj.Name()) {
			for _, name := range declared {
				if key == name {
					continue // the exact name of a declared key passes
				}
				if canonicalKey(key) == canonicalKey(name) {
					nearMiss = key + " (the typed key is " + name + ")"
					return false
				}
			}
		}
		return true
	})
	if nearMiss != "" {
		return fail(RuleKeysStrict, WeightKeysStrict, "near-miss key: "+nearMiss)
	}
	return pass(RuleKeysStrict, WeightKeysStrict)
}

// declaredKeys returns every key name the package declares with wlog.NewKey, in a stable
// order so a finding never depends on map iteration.
func declaredKeys(pkg *packages.Package) []string {
	var names []string
	seen := map[string]bool{}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isNewKeyCall(pkg, call) || len(call.Args) != 1 {
				return true
			}
			for _, name := range literalStrings(call.Args[0]) {
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
			return true
		})
	}
	return names
}

// isNewKeyCall reports whether a call is wlog.NewKey, however its type argument is written.
func isNewKeyCall(pkg *packages.Package, call *ast.CallExpr) bool {
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
	obj, _ := pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == wlogPath && obj.Name() == "NewKey"
}

// canonicalKey folds the spellings an author mixes up: case, and the separators wlog splits a
// key on. So orderID and order_id share one form, and so do api-key and APIKey.
func canonicalKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		if r == '_' || r == '-' || r == ' ' || r == '.' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
