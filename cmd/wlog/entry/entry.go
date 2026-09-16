// Package entry finds HTTP handler entry points in Go packages. It reads type
// information only, so it knows a framework by package path and never imports it.
package entry

import (
	"go/ast"
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
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedModule,
	}
	return packages.Load(cfg, patterns...)
}

// Find returns every entry point in pkgs, sorted so the map is deterministic.
func Find(pkgs []*packages.Package) []Point {
	var points []Point
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			points = append(points, findInFile(pkg, file)...)
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

// registration describes one framework call that registers a handler.
type registration struct {
	pkgPath string
	name    string
	method  string // the HTTP method, when the function name is the method
	handle  bool   // Handle(method, path, handlers): the method is the first argument
	pathArg int
}

// findInFile walks one file and returns the entry points it registers.
func findInFile(pkg *packages.Package, file *ast.File) []Point {
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
			point, ok := pointFor(pkg, fn, call, reg)
			if !ok {
				return true
			}
			byCall[call] = point
			return true
		})
	}

	applyMethodChains(pkg, file, byCall)

	points := make([]Point, 0, len(byCall))
	for _, point := range byCall {
		points = append(points, *point)
	}
	return points
}

// applyMethodChains reads mux's chained .Methods("GET") and writes the method onto the
// registration it was chained from.
func applyMethodChains(pkg *packages.Package, file *ast.File, byCall map[*ast.CallExpr]*Point) {
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Methods" || len(call.Args) == 0 {
			return true
		}
		obj, ok := pkg.TypesInfo.Uses[sel.Sel].(*types.Func)
		if !ok || obj.Pkg() == nil || obj.Pkg().Path() != muxPath {
			return true
		}
		inner, ok := sel.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		if point := byCall[inner]; point != nil {
			point.Method = stringLit(call.Args[0])
		}
		return true
	})
}

// matchRegistration reports whether call registers a handler, and with which rule.
func matchRegistration(pkg *packages.Package, call *ast.CallExpr, regs []registration) (registration, bool) {
	obj := calleeObject(pkg, call)
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

// calleeObject resolves a call's callee to a function or method.
func calleeObject(pkg *packages.Package, call *ast.CallExpr) *types.Func {
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

// pointFor builds a Point for one registration call. It reports false when the
// handler argument is not a plain function literal or a named function.
func pointFor(pkg *packages.Package, enclosing *ast.FuncDecl, call *ast.CallExpr, reg registration) (*Point, bool) {
	if len(call.Args) == 0 {
		return nil, false
	}
	handlerArg := call.Args[len(call.Args)-1]
	function, pos, ok := handlerTarget(pkg, handlerArg)
	if !ok {
		return nil, false
	}
	if function == "" {
		function = enclosing.Name.Name
	}

	method, route := routeFor(call, reg)
	position := pkg.Fset.Position(pos)
	return &Point{
		Package:   pkg.PkgPath,
		Function:  function,
		File:      filepath.Base(position.Filename),
		Line:      position.Line,
		Framework: reg.pkgPath,
		Method:    method,
		Route:     route,
		Node:      handlerArg,
	}, true
}

// handlerTarget names the handler: a function literal reports the enclosing function
// and the literal's own position, a named function reports its name and declaration.
func handlerTarget(pkg *packages.Package, arg ast.Expr) (string, token.Pos, bool) {
	switch expr := arg.(type) {
	case *ast.FuncLit:
		return "", expr.Pos(), true
	case *ast.Ident:
		if fn, ok := pkg.TypesInfo.Uses[expr].(*types.Func); ok {
			return fn.Name(), fn.Pos(), true
		}
	case *ast.SelectorExpr:
		if fn, ok := pkg.TypesInfo.Uses[expr.Sel].(*types.Func); ok {
			return fn.Name(), fn.Pos(), true
		}
	}
	return "", token.NoPos, false
}

// routeFor reads the method and route from a registration call.
func routeFor(call *ast.CallExpr, reg registration) (string, string) {
	args := call.Args
	if reg.handle {
		if len(args) < 3 {
			return "", ""
		}
		return stringLit(args[0]), stringLit(args[1])
	}
	route := stringLit(args[reg.pathArg])
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

// stringLit reads a string-literal argument, or returns "" for anything else.
func stringLit(expr ast.Expr) string {
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
