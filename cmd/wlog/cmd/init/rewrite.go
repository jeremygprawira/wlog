package init

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"strconv"
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

	switch framework {
	case "nethttp", "mux":
		// Both start a server with a handler: net/http's ListenAndServe, or an http.Server
		// literal whose Handler field names one.
		if !wrapHandlers(file) {
			return nil, false, nil
		}
	default:
		// Splice the call as text. An inserted syntax node has no position, so the printer
		// pulls the next comment into the call.
		return spliceRouterUse(fset, file, source, framework)
	}

	var out bytes.Buffer
	config := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := config.Fprint(&out, fset, file); err != nil {
		return nil, false, fmt.Errorf("print %s: %w", filename, err)
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return nil, false, fmt.Errorf("format %s: %w", filename, err)
	}
	return formatted, true, nil
}

// spliceRouterUse inserts router.Use(LoggerMiddleware()) at the end of the statement that
// builds the router, then formats the file.
func spliceRouterUse(fset *token.FileSet, file *ast.File, source []byte, framework string) ([]byte, bool, error) {
	offset, router, ok := routerInsert(fset, file, framework)
	if !ok {
		return nil, false, nil
	}
	insertion := []byte("\n" + router + ".Use(LoggerMiddleware())")
	spliced := make([]byte, 0, len(source)+len(insertion))
	spliced = append(spliced, source[:offset]...)
	spliced = append(spliced, insertion...)
	spliced = append(spliced, source[offset:]...)
	formatted, err := format.Source(spliced)
	if err != nil {
		return nil, false, fmt.Errorf("format spliced source: %w", err)
	}
	return formatted, true, nil
}

// routerInsert returns the byte offset just after the router is built.
func routerInsert(fset *token.FileSet, file *ast.File, framework string) (int, string, bool) {
	var offset int
	var router string
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if found {
			return false
		}
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for _, statement := range block.List {
			name, ok := routerName(statement, framework)
			if !ok || hasWlogMiddleware(file, block.List, name) {
				continue
			}
			offset = fset.Position(statement.End()).Offset
			router = name
			found = true
			return false
		}
		return true
	})
	return offset, router, found
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

// hasWlogMiddleware reports whether this router already calls wlog. Another Use, such as
// a recoverer, is not wlog and does not count.
func hasWlogMiddleware(file *ast.File, statements []ast.Stmt, router string) bool {
	names := wlogImportNames(file)
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
		receiver, ok := sel.X.(*ast.Ident)
		if !ok || receiver.Name != router {
			continue
		}
		if callUsesWlog(expr, names) {
			return true
		}
	}
	return false
}

// wlogImportNames maps a local name to true when that import is a wlog package.
func wlogImportNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !strings.Contains(path, "jeremygprawira/wlog") {
			continue
		}
		local := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil && spec.Name.Name != "_" && spec.Name.Name != "." {
			local = spec.Name.Name
		}
		names[local] = true
	}
	return names
}

// callUsesWlog reports whether a Use call's arguments name wlog.
func callUsesWlog(call *ast.CallExpr, names map[string]bool) bool {
	for _, arg := range call.Args {
		if exprUsesWlog(arg, names) {
			return true
		}
	}
	return false
}

// exprUsesWlog reports whether an expression is a wlog call or a generated wrapper.
func exprUsesWlog(expr ast.Expr, names map[string]bool) bool {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name == "LoggerMiddleware" || node.Name == "WrapHandler"
	case *ast.SelectorExpr:
		if ident, ok := node.X.(*ast.Ident); ok && names[ident.Name] {
			return true
		}
		return node.Sel.Name == "LoggerMiddleware" || node.Sel.Name == "WrapHandler"
	case *ast.CallExpr:
		return exprUsesWlog(node.Fun, names)
	default:
		return false
	}
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
	case "chi":
		return pkg.Name == "chi" && sel.Sel.Name == "NewRouter"
	case "fiber", "fiber3":
		return pkg.Name == "fiber" && (sel.Sel.Name == "New" || sel.Sel.Name == "Default")
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
