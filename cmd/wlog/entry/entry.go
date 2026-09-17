// Package entry finds HTTP handler entry points in Go packages. It reads type
// information only, so it knows a framework by package path and never imports it.
package entry

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"

	"golang.org/x/tools/go/packages"
)

// Point is one place a handler is registered. Node is the handler's AST node, used by
// the rules package to inspect the body.
type Point struct {
	Package   string
	Function  string
	File      string
	Line      int
	Framework string
	Method    string
	Route     string
	Sensitive bool
	Node      ast.Node `json:"-"`
}

// Load loads packages for patterns with the syntax and type information the finder
// needs.
func Load(patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		// NeedImports is what makes NeedDeps useful: without it a dependency never reaches
		// pkg.Imports, and a handler declared in another package cannot be resolved.
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedDeps | packages.NeedModule,
	}
	return packages.Load(cfg, patterns...)
}

// Find returns every entry point in pkgs, sorted so the map is deterministic.
func Find(pkgs []*packages.Package) []Point {
	// The search space is every loaded package, the requested ones AND their dependencies:
	// a handler declared in another package is a dependency, and its syntax is only reachable
	// through the import graph.
	all := allPackages(pkgs)

	var points []Point
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			points = append(points, findInFile(all, pkg, file)...)
		}
	}
	sort.Slice(points, func(i, j int) bool {
		a, b := points[i], points[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Function != b.Function {
			return a.Function < b.Function
		}
		return a.Route < b.Route
	})
	return points
}

// allPackages returns every loaded package: the requested ones and the dependencies the
// loader kept syntax for, walked through the import graph.
func allPackages(pkgs []*packages.Package) []*packages.Package {
	seen := map[string]bool{}
	var out []*packages.Package
	var walk func(pkg *packages.Package)
	walk = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true
		out = append(out, pkg)
		for _, dep := range pkg.Imports {
			walk(dep)
		}
	}
	for _, pkg := range pkgs {
		walk(pkg)
	}
	return out
}

// registration describes one framework call that registers a handler.
type registration struct {
	pkgPath string
	name    string
	method  string // the HTTP method, when the function name is the method
	handle  bool   // Handle(method, path, handlers): the method is the first argument
	pathArg int
}

// findInFile walks one file and returns the entry points it registers.
func findInFile(pkgs []*packages.Package, pkg *packages.Package, file *ast.File) []Point {
	regs := registrations()
	byCall := map[*ast.CallExpr]*Point{}

	// First pass: registrations, keyed by their call expression, so a chained
	// .Methods("GET") can find them in the second pass.
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			reg, ok := matchRegistration(pkg, call, regs)
			if !ok {
				return true
			}
			point, ok := pointFor(pkgs, pkg, fn, call, reg)
			if !ok {
				return true
			}
			byCall[call] = point
			return true
		})
	}

	applyRouteChains(pkg, file, byCall)

	points := make([]Point, 0, len(byCall))
	for _, point := range byCall {
		points = append(points, *point)
	}
	return points
}

// applyRouteChains reads mux's chained .Methods("GET") and .Path("/orders"), in either
// direction: r.Methods("GET").Path("/x").HandlerFunc(h) carries the chain on the receiver of
// the registration, while r.HandleFunc("/x", h).Methods("GET") wraps it.
func applyRouteChains(pkg *packages.Package, file *ast.File, byCall map[*ast.CallExpr]*Point) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Methods" && sel.Sel.Name != "Path") || len(call.Args) == 0 {
			return true
		}
		obj := Callee(pkg, call)
		if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != MuxPath {
			return true
		}
		inner := innermostRegistration(sel.X, byCall)
		if inner == nil {
			return true
		}
		point := byCall[inner]
		if point == nil {
			return true
		}
		switch {
		case sel.Sel.Name == "Methods" && point.Method == "":
			point.Method = stringValue(pkg, call.Args[0])
		case sel.Sel.Name == "Path" && point.Route == "":
			point.Route = stringValue(pkg, call.Args[0])
		}
		return true
	})

	// And the chain a registration's own receiver carries.
	for call, point := range byCall {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		method, route := chainRoute(pkg, sel.X)
		if method != "" && point.Method == "" {
			point.Method = method
		}
		if route != "" && point.Route == "" {
			point.Route = route
		}
	}
}

// innermostRegistration returns the registration call a chain wraps, or nil when the chain
// holds none.
func innermostRegistration(expr ast.Expr, byCall map[*ast.CallExpr]*Point) *ast.CallExpr {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return nil
		}
		if _, isRegistration := byCall[call]; isRegistration {
			return call
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil
		}
		expr = sel.X
	}
}

// chainRoute walks a mux route's receiver chain and returns the method and the path it names,
// however many calls deep they sit.
func chainRoute(pkg *packages.Package, expr ast.Expr) (method, route string) {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return method, route
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return method, route
		}
		if obj := Callee(pkg, call); obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == MuxPath {
			switch sel.Sel.Name {
			case "Methods":
				if method == "" && len(call.Args) > 0 {
					method = stringValue(pkg, call.Args[0])
				}
			case "Path":
				if route == "" && len(call.Args) > 0 {
					route = stringValue(pkg, call.Args[0])
				}
			}
		}
		expr = sel.X
	}
}

// matchRegistration reports whether call registers a handler, and with which rule.
func matchRegistration(pkg *packages.Package, call *ast.CallExpr, regs []registration) (registration, bool) {
	obj := Callee(pkg, call)
	if obj == nil || obj.Pkg() == nil {
		return registration{}, false
	}
	for _, reg := range regs {
		if obj.Pkg().Path() == reg.pkgPath && obj.Name() == reg.name {
			return reg, true
		}
	}
	return registration{}, false
}

// Callee resolves a call's callee to a function or method. It returns nil when the
// callee is not a plain function or method.
func Callee(pkg *packages.Package, call *ast.CallExpr) *types.Func {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		obj, _ := pkg.TypesInfo.Uses[fn].(*types.Func)
		return obj
	case *ast.SelectorExpr:
		if obj, ok := pkg.TypesInfo.Uses[fn.Sel].(*types.Func); ok {
			return obj
		}
		if sel, ok := pkg.TypesInfo.Selections[fn]; ok {
			if obj, ok := sel.Obj().(*types.Func); ok {
				return obj
			}
		}
	}
	return nil
}

// pointFor builds a Point for one registration call. It reports false when the handler
// argument is not something this package can follow to the code that runs.
func pointFor(pkgs []*packages.Package, pkg *packages.Package, enclosing *ast.FuncDecl, call *ast.CallExpr, reg registration) (*Point, bool) {
	if len(call.Args) == 0 {
		return nil, false
	}
	handlerArg := call.Args[len(call.Args)-1]
	target, ok := handlerTarget(pkgs, pkg, handlerArg)
	if !ok {
		return nil, false
	}
	if target.name == "" {
		target.name = enclosing.Name.Name
	}

	// The point names the package the handler is DECLARED in, which is often not the package
	// that registers it: a rule reads the handler's own body, and the report must point at the
	// file a reader has to open.
	owner := pkg
	if target.pkg != nil {
		owner = target.pkg
	}
	position := owner.Fset.Position(target.node.Pos())

	method, route := routeFor(pkg, call, reg)
	return &Point{
		Package:   owner.PkgPath,
		Function:  target.name,
		File:      filepath.Base(position.Filename),
		Line:      position.Line,
		Framework: reg.pkgPath,
		Method:    method,
		Route:     route,
		Node:      target.node,
	}, true
}

// handlerTargetInfo is the code behind one handler argument.
type handlerTargetInfo struct {
	name string            // the function or method name, empty for a literal
	node ast.Node          // the declaration or literal a rule inspects
	pkg  *packages.Package // the package it is declared in, nil for a literal
}

// handlerTarget resolves a handler argument to the code that will run, wherever that code
// lives. It follows a function literal, a named function or method in ANY loaded package, a
// conversion such as http.HandlerFunc(fn), a call to a factory that returns a handler (s.refund()),
// and a value whose type has a ServeHTTP method.
func handlerTarget(pkgs []*packages.Package, pkg *packages.Package, arg ast.Expr) (handlerTargetInfo, bool) {
	switch expr := arg.(type) {
	case *ast.FuncLit:
		return handlerTargetInfo{node: expr}, true
	case *ast.CallExpr:
		// A conversion wraps the value inside it: http.HandlerFunc(fn) runs fn.
		if len(expr.Args) == 1 && isConversion(pkg, expr) {
			return handlerTarget(pkgs, pkg, expr.Args[0])
		}
		// A pass-through adapter hands its argument to the framework, so the handler a rule
		// reads is that argument, not the adapter.
		if len(expr.Args) > 0 && isPassThrough(Callee(pkg, expr)) {
			return handlerTarget(pkgs, pkg, expr.Args[0])
		}
		// A factory call returns the handler, so the factory is where a rule must look.
		if fn := Callee(pkg, expr); fn != nil {
			if target, ok := funcTarget(pkgs, fn); ok {
				return target, true
			}
		}
	case *ast.UnaryExpr:
		// &myHandler{} is a value, and its type may carry ServeHTTP.
		return handlerTarget(pkgs, pkg, expr.X)
	case *ast.CompositeLit:
		if target, ok := serveHTTPTarget(pkgs, pkg, pkg.TypesInfo.TypeOf(expr)); ok {
			return target, true
		}
	case *ast.Ident:
		if fn, ok := pkg.TypesInfo.Uses[expr].(*types.Func); ok {
			return funcTarget(pkgs, fn)
		}
		if target, ok := serveHTTPTarget(pkgs, pkg, pkg.TypesInfo.TypeOf(expr)); ok {
			return target, true
		}
	case *ast.SelectorExpr:
		if fn, ok := pkg.TypesInfo.Uses[expr.Sel].(*types.Func); ok {
			return funcTarget(pkgs, fn)
		}
		if target, ok := serveHTTPTarget(pkgs, pkg, pkg.TypesInfo.TypeOf(expr)); ok {
			return target, true
		}
	}
	return handlerTargetInfo{}, false
}

// passThrough names the adapter functions that do nothing but hand their first argument to a
// framework. Following them is what lets the map see the handler a reader wrote rather than the
// wrapper library that carries it.
var passThrough = map[string]map[string]bool{
	Echo4Path: {"WrapHandler": true},
	Echo5Path: {"WrapHandler": true},
	GinPath:   {"WrapH": true},
}

// isPassThrough reports whether fn is one of those adapters.
func isPassThrough(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	return passThrough[fn.Pkg().Path()][fn.Name()]
}

// isConversion reports whether a one-argument call is a type conversion rather than a call.
func isConversion(pkg *packages.Package, call *ast.CallExpr) bool {
	return pkg.TypesInfo.Types[call.Fun].IsType()
}

// funcTarget resolves a named function or method to its declaration in any loaded package,
// which is what makes a handler declared in another package visible.
func funcTarget(pkgs []*packages.Package, fn *types.Func) (handlerTargetInfo, bool) {
	owner, decl := declForAny(pkgs, fn.Pos())
	if decl == nil {
		return handlerTargetInfo{}, false
	}
	return handlerTargetInfo{name: fn.Name(), node: decl, pkg: owner}, true
}

// serveHTTPTarget resolves a value whose type implements http.Handler, reporting the
// ServeHTTP declaration so a rule reads the handler's own body rather than the type name.
func serveHTTPTarget(pkgs []*packages.Package, pkg *packages.Package, t types.Type) (handlerTargetInfo, bool) {
	if t == nil {
		return handlerTargetInfo{}, false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return handlerTargetInfo{}, false
	}
	for i := 0; i < named.NumMethods(); i++ {
		if method := named.Method(i); method.Name() == "ServeHTTP" {
			return funcTarget(pkgs, method)
		}
	}
	// The method may be declared on the pointer receiver only.
	if pkg != nil && pkg.Types != nil {
		if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(named), true, pkg.Types, "ServeHTTP"); obj != nil {
			if method, ok := obj.(*types.Func); ok {
				return funcTarget(pkgs, method)
			}
		}
	}
	return handlerTargetInfo{}, false
}

// declForAny finds the declaration of a named function at pos, in any loaded package. The
// loader asks for its dependencies' syntax too, so a handler in another package is here.
func declForAny(pkgs []*packages.Package, pos token.Pos) (*packages.Package, *ast.FuncDecl) {
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Pos() == pos {
					return pkg, fn
				}
			}
		}
	}
	return nil, nil
}

// routeFor reads the method and route from a registration call.
func routeFor(pkg *packages.Package, call *ast.CallExpr, reg registration) (string, string) {
	args := call.Args
	if reg.handle {
		if len(args) < 3 {
			return "", ""
		}
		return stringValue(pkg, args[0]), stringValue(pkg, args[1])
	}
	route := stringValue(pkg, args[reg.pathArg])
	if reg.method != "" {
		return reg.method, route
	}
	return splitMethodPattern(route)
}

// splitMethodPattern splits a net/http or mux pattern that starts with an HTTP
// method, for example "GET /orders/{id}".
func splitMethodPattern(pattern string) (string, string) {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == ' ' {
			return pattern[:i], pattern[i+1:]
		}
	}
	return "", pattern
}

// stringValue reads a constant string expression: a literal, or a named constant such as
// http.MethodPost. Anything else reads as "", so an unknown route is never invented.
func stringValue(pkg *packages.Package, expr ast.Expr) string {
	if value := pkg.TypesInfo.Types[expr].Value; value != nil && value.Kind() == constant.String {
		return constant.StringVal(value)
	}
	return StringLiteral(expr)
}

// StringLiteral reads a string-literal argument, or returns "" for anything else.
func StringLiteral(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}
