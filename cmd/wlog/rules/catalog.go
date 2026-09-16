package rules

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// useCatalog suggests replacing a literal error code with the catalog entry that
// already holds it. It stays quiet until a registry in the same package holds the
// literal, because a suggestion without evidence is noise.
func useCatalog(pkg *packages.Package, point entry.Point) Check {
	codes := catalogCodes(pkg)
	if len(codes) == 0 {
		return passSuggestion(RuleUseCatalog)
	}
	body := bodyOf(point.Node)
	if body == nil {
		return passSuggestion(RuleUseCatalog)
	}
	found := ""
	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		if value := entry.StringLiteral(literal); codes[value] {
			found = value
			return false
		}
		return true
	})
	if found != "" {
		return suggest(RuleUseCatalog, "literal "+found+" is already in the catalog")
	}
	return passSuggestion(RuleUseCatalog)
}

// catalogCodes reads every code a catalog.New call in the package registers.
func catalogCodes(pkg *packages.Package) map[string]bool {
	codes := map[string]bool{}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee := entry.Callee(pkg, call)
			if callee == nil || callee.Pkg() == nil || callee.Pkg().Path() != catalogPath || callee.Name() != "New" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			prefix := entry.StringLiteral(call.Args[0])
			if prefix == "" {
				return true
			}
			for _, argument := range call.Args[1:] {
				composite, ok := argument.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, element := range composite.Elts {
					pair, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := pair.Key.(*ast.Ident)
					if !ok || key.Name != "Code" {
						continue
					}
					if code := entry.StringLiteral(pair.Value); code != "" {
						codes[strings.ToUpper(prefix)+"_"+strings.ToUpper(code)] = true
					}
				}
			}
			return true
		})
	}
	return codes
}
