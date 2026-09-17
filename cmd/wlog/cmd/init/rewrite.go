package init

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// rewrite returns source with the wlog middleware installed. It parses the file and changes the
// syntax tree, so the change lands where it belongs whatever the file's formatting is: a regex
// rewrite of Go source is a guess about whitespace, and this is not a guess.
//
// It reports false when the file holds nothing to change.
func rewrite(filename string, source []byte, framework string) ([]byte, bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", filename, err)
	}

	var changed bool
	switch framework {
	case "nethttp", "mux":
		// Both start a server with a handler: net/http's ListenAndServe, or an http.Server
		// literal whose Handler field names one.
		changed = wrapHandlers(file)
	default:
		changed = installRouterMiddleware(file, framework)
	}
	if !changed {
		return nil, false, nil
	}

	var out bytes.Buffer
	config := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := config.Fprint(&out, fset, file); err != nil {
		return nil, false, fmt.Errorf("print %s: %w", filename, err)
	}
	return out.Bytes(), true, nil
}

// wrapHandlers wraps every server handler in the file with WrapHandler.
//
// A nil handler means the default mux, which is what the app really serves, so the wrapper goes
// around that instead of around nil.
func wrapHandlers(file *ast.File) bool {
	changed := false
	ast.Inspect(file, func(node ast.Node) bool {
		switch expr := node.(type) {
		case *ast.CallExpr:
			if !isListenAndServe(expr) || len(expr.Args) < 2 {
				return true
			}
			expr.Args[1] = wrapHandlerArg(expr.Args[1])
			changed = true
		case *ast.CompositeLit:
			if !isHTTPServerLiteral(expr) {
				return true
			}
			for _, element := range expr.Elts {
				kv, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Handler" {
					continue
				}
				kv.Value = wrapHandlerArg(kv.Value)
				changed = true
			}
		}
		return true
	})
	return changed
}

// wrapHandlerArg returns the argument wrapped in WrapHandler, turning a literal nil into the
// default mux first.
func wrapHandlerArg(arg ast.Expr) ast.Expr {
	if isNilIdent(arg) {
		return callExpr("WrapHandler", selectorExpr("http", "DefaultServeMux"))
	}
	if isWrapHandlerCall(arg) {
		return arg
	}
	return callExpr("WrapHandler", arg)
}

// installRouterMiddleware inserts router.Use(LoggerMiddleware()) after the router is built, so
// every route the file registers is covered.
func installRouterMiddleware(file *ast.File, framework string) bool {
	changed := false
	ast.Inspect(file, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, statement := range block.List {
			router, ok := routerName(statement, framework)
			if !ok || hasMiddleware(block.List, router) {
				continue
			}
			insert := i + 1
			updated := make([]ast.Stmt, 0, len(block.List)+1)
			updated = append(updated, block.List[:insert]...)
			updated = append(updated, middlewareStatement(router))
			updated = append(updated, block.List[insert:]...)
			block.List = updated
			changed = true
			break
		}
		return true
	})
	return changed
}

// routerName returns the variable a statement assigns a router to, when the framework matches.
func routerName(statement ast.Stmt, framework string) (string, bool) {
	assign, ok := statement.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return "", false
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || !isRouterConstructor(call, framework) {
		return "", false
	}
	return ident.Name, true
}

// hasMiddleware reports whether the block already calls Use on this router, so a second run of
// the tool cannot add a second wrapper.
func hasMiddleware(statements []ast.Stmt, router string) bool {
	for _, statement := range statements {
		call, ok := statement.(*ast.ExprStmt)
		if !ok {
			continue
		}
		expr, ok := call.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := expr.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Use" {
			continue
		}
		if receiver, ok := sel.X.(*ast.Ident); ok && receiver.Name == router {
			return true
		}
	}
	return false
}

// middlewareStatement builds router.Use(LoggerMiddleware()).
func middlewareStatement(router string) ast.Stmt {
	return &ast.ExprStmt{X: callExpr(router+".Use", callExpr("LoggerMiddleware"))}
}

// isListenAndServe reports whether a call starts an http server.
func isListenAndServe(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "ListenAndServe" && sel.Sel.Name != "ListenAndServeTLS") {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "http"
}

// isHTTPServerLiteral reports whether a composite literal builds an http.Server.
func isHTTPServerLiteral(lit *ast.CompositeLit) bool {
	switch t := lit.Type.(type) {
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		return ok && pkg.Name == "http" && t.Sel.Name == "Server"
	case *ast.StarExpr:
		if sel, ok := t.X.(*ast.SelectorExpr); ok {
			pkg, ok := sel.X.(*ast.Ident)
			return ok && pkg.Name == "http" && sel.Sel.Name == "Server"
		}
	}
	return false
}

// isRouterConstructor reports whether a call builds the router of this framework.
func isRouterConstructor(call *ast.CallExpr, framework string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch framework {
	case "echo", "echo5":
		return pkg.Name == "echo" && sel.Sel.Name == "New"
	case "gin":
		return pkg.Name == "gin" && (sel.Sel.Name == "New" || sel.Sel.Name == "Default")
	default:
		return false
	}
}

// isNilIdent reports whether an expression is the literal nil.
func isNilIdent(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

// isWrapHandlerCall reports whether an expression is already wrapped.
func isWrapHandlerCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "WrapHandler"
}

// callExpr builds a call expression from a dotted name and its arguments.
func callExpr(name string, args ...ast.Expr) *ast.CallExpr {
	parts := strings.Split(name, ".")
	var fun ast.Expr
	switch len(parts) {
	case 1:
		fun = ast.NewIdent(parts[0])
	default:
		fun = selectorExpr(strings.Join(parts[:len(parts)-1], "."), parts[len(parts)-1])
	}
	return &ast.CallExpr{Fun: fun, Args: args}
}

// selectorExpr builds x.Sel.
func selectorExpr(pkg, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: ast.NewIdent(pkg), Sel: ast.NewIdent(name)}
}
