package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// osPath is the os package the rule recognizes without importing it.
const osPath = "os"

// noPrint passes when the handler does not log to stdout, to stderr, or through the standard
// logger. A handler should put its data on the event.
//
// A write through a writer, such as fmt.Fprintf(w, ...) or json.NewEncoder(w).Encode(v), is not
// print logging: the response is where that data belongs (CLI-14).
func noPrint(pkg *packages.Package, point entry.Point) Check {
	body := bodyOf(point.Node)
	if body == nil {
		return pass(RuleNoPrint, WeightNoPrint)
	}
	var at ast.Node
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		obj := entry.Callee(pkg, call)
		if obj == nil || obj.Pkg() == nil {
			return true
		}
		if isPrintCall(pkg, call, obj.Pkg().Path(), obj.Name()) {
			at = call
			return false
		}
		return true
	})
	if at != nil {
		check := fail(RuleNoPrint, WeightNoPrint, "handler uses print logging")
		check.Node = at
		return check
	}
	return pass(RuleNoPrint, WeightNoPrint)
}

// isPrintCall reports whether a call writes to stdout, to stderr, or through the standard
// logger.
func isPrintCall(pkg *packages.Package, call *ast.CallExpr, pkgPath, name string) bool {
	switch pkgPath {
	case "fmt":
		switch name {
		case "Print", "Printf", "Println":
			return true // these three write to stdout
		case "Fprint", "Fprintf", "Fprintln":
			// These write to the first argument, which only counts when it is os.Stdout or
			// os.Stderr.
			return len(call.Args) > 0 && isStandardStream(pkg, call.Args[0])
		}
	case "log":
		switch name {
		case "Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln":
			return true
		}
	case osPath:
		// os.Stdout.Write / os.Stderr.Write
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if inner, ok := sel.X.(*ast.SelectorExpr); ok && isStandardStream(pkg, inner) {
				return name == "Write" || name == "WriteString"
			}
		}
	}
	return false
}

// isStandardStream reports whether an expression is os.Stdout or os.Stderr.
func isStandardStream(pkg *packages.Package, expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Stdout" && sel.Sel.Name != "Stderr" {
		return false
	}
	obj, _ := pkg.TypesInfo.Uses[sel.Sel].(*types.Var)
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == osPath
}
