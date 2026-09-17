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
	"strings"

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

	// extraMethods holds the other methods a mux chain named, such as .Methods("GET", "POST").
	// One route with two methods scores as two entries, so a write route is never read as a
	// read route.
	extraMethods []string
	// routePrefix is the group prefix the registering router carries. The join happens once,
	// after the chain has supplied the route, because a mux route names its path in the chain
	// rather than in the registration call.
	routePrefix string
}

// expand returns this point plus one entry per extra method the route named.
func (p Point) expand() []Point {
	points := []Point{p}
	for _, method := range p.extraMethods {
		extra := p
		extra.Method = method
		extra.extraMethods = nil
		points = append(points, extra)
	}
	return points
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

// Program returns every loaded package: the requested ones and the dependencies the loader
// kept syntax for, walked through the import graph. A rule that must look beyond one package,
// such as middleware coverage, searches this.
func Program(pkgs []*packages.Package) []*packages.Package { return allPackages(pkgs) }

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
	// handlerArg names the argument holding the handler when it is not the last one. Zero
	// means the last argument, which is the shape net/http, mux, and Gin use. Echo puts the
	// handler before the per-route middleware that follows it.
	handlerArg int
}

// findInFile walks one file and returns the entry points it registers.
func findInFile(pkgs []*packages.Package, pkg *packages.Package, file *ast.File) []Point {
	regs := registrations()
	byCall := map[*ast.CallExpr]*Point{}
	prefixes := groupPrefixes(pkg, file)

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
			point, ok := pointFor(pkgs, pkg, fn, call, reg, prefixes)
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
		// The prefix joins the route here, once both are known: the registration call may name
		// its path, or the chain may.
		point.Route = joinRoute(point.routePrefix, point.Route)
		point.routePrefix = ""
		points = append(points, point.expand()...)
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
		case sel.Sel.Name == "Methods":
			var methods []string
			for _, arg := range call.Args {
				if value := stringValue(pkg, arg); value != "" {
					methods = append(methods, value)
				}
			}
			applyMethods(point, methods)
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
		methods, route := chainRoute(pkg, sel.X)
		applyMethods(point, methods)
		if route != "" && point.Route == "" {
			point.Route = route
		}
	}
}

// applyMethods writes the methods a chain named onto one point: the first is the point's own
// method, and the rest become entries of their own, so one route with two methods scores twice.
func applyMethods(point *Point, methods []string) {
	if len(methods) == 0 || point.Method != "" {
		return
	}
	point.Method = methods[0]
	point.extraMethods = append(point.extraMethods, methods[1:]...)
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

// chainRoute walks a route's receiver chain and returns the methods and the path it names,
// however many calls deep they sit.
func chainRoute(pkg *packages.Package, expr ast.Expr) (methods []string, route string) {
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return methods, route
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return methods, route
		}
		if obj := Callee(pkg, call); obj != nil && obj.Pkg() != nil {
			name := sel.Sel.Name
			switch {
			case obj.Pkg().Path() == MuxPath && (name == "Methods" || name == "Path"):
				for _, arg := range call.Args {
					if value := stringValue(pkg, arg); value != "" {
						if name == "Methods" {
							methods = append(methods, value)
						} else if route == "" {
							route = value
						}
					}
				}
			case obj.Pkg().Path() == MuxPath && name == "PathPrefix":
				if route == "" && len(call.Args) > 0 {
					route = stringValue(pkg, call.Args[0])
				}
			}
		}
		expr = sel.X
	}
}

// groupPrefixes maps a router variable to the prefix its Group or Subrouter call added, so a
// route registered on a group keeps /admin in front of it.
func groupPrefixes(pkg *packages.Package, file *ast.File) map[string]string {
	prefixes := map[string]string{}
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		name, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if prefix, ok := prefixOfCall(pkg, assign.Rhs[0], prefixes); ok {
			prefixes[name.Name] = prefix
		}
		return true
	})
	return prefixes
}

// prefixOfCall returns the prefix a Group or Subrouter call adds, joined to the prefix its
// receiver already carried.
func prefixOfCall(pkg *packages.Package, expr ast.Expr, prefixes map[string]string) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	obj := Callee(pkg, call)
	if obj == nil || obj.Pkg() == nil {
		return "", false
	}

	switch {
	case obj.Name() == "Group" && (obj.Pkg().Path() == Echo4Path || obj.Pkg().Path() == Echo5Path || obj.Pkg().Path() == GinPath):
		if len(call.Args) == 0 {
			return "", false
		}
		return joinRoute(prefixOfReceiver(sel.X, prefixes), stringValue(pkg, call.Args[0])), true
	case obj.Name() == "Subrouter" && obj.Pkg().Path() == MuxPath:
		// router.PathPrefix("/admin").Subrouter()
		_, route := chainRoute(pkg, sel.X)
		return joinRoute(prefixOfReceiver(sel.X, prefixes), route), true
	}
	return "", false
}

// prefixOfReceiver returns the prefix a router variable already carries.
func prefixOfReceiver(expr ast.Expr, prefixes map[string]string) string {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return prefixes[ident.Name]
}

// groupPrefixOf returns the prefix the registration's router carries.
//
// The router is the variable at the start of the receiver chain, which a mux route hides behind
// its Methods and Path calls: adminRouter.Methods("GET").Path("/logs").HandlerFunc(h).
func groupPrefixOf(pkg *packages.Package, call *ast.CallExpr, prefixes map[string]string) string {
	if len(prefixes) == 0 {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	expr := sel.X
	for {
		inner, ok := expr.(*ast.CallExpr)
		if !ok {
			break
		}
		innerSel, ok := inner.Fun.(*ast.SelectorExpr)
		if !ok {
			break
		}
		expr = innerSel.X
	}
	return prefixOfReceiver(expr, prefixes)
}

// joinRoute joins a group prefix and a route, leaving exactly one slash between them.
func joinRoute(prefix, route string) string {
	if prefix == "" {
		return route
	}
	if route == "" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(route, "/")
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
func pointFor(pkgs []*packages.Package, pkg *packages.Package, enclosing *ast.FuncDecl, call *ast.CallExpr, reg registration, prefixes map[string]string) (*Point, bool) {
	if len(call.Args) == 0 {
		return nil, false
	}
	argIndex := len(call.Args) - 1
	if reg.handlerArg > 0 && reg.handlerArg < len(call.Args) {
		// Echo takes the handler before its per-route middleware, so the last argument may be
		// a middleware function rather than the handler.
		argIndex = reg.handlerArg
	}
	handlerArg := call.Args[argIndex]
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
		Package:     owner.PkgPath,
		Function:    target.name,
		File:        filepath.Base(position.Filename),
		Line:        position.Line,
		Framework:   reg.pkgPath,
		Method:      method,
		Route:       route,
		Node:        target.node,
		routePrefix: groupPrefixOf(pkg, call, prefixes),
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
