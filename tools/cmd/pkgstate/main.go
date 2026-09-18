// Command pkgstate fails when a Go package holds package-level state, which is a
// variable that some code writes.
//
// wlog allows exactly one piece of package-level state: the pointer behind SetDefault
// and Default in package wlog. Every other package reads its data from a value or from
// the context, so two tests or two services in one process never share a variable.
//
// A variable counts as state when its type is a lock or an atomic, or when some code
// writes it, by assignment or by a method call. A read-only table passes, because a
// reader may not change it, and the command keeps the exception list to that one
// pointer.
//
// Run it from the tools directory: go run ./cmd/pkgstate
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// allowed names the package-level variables the project allows. It holds one entry: the
// default Logger pointer of package wlog.
var allowed = map[string]bool{"defaultLogger": true}

// skipDirs names the directories the walk does not enter.
var skipDirs = map[string]bool{"testdata": true, "vendor": true, "node_modules": true}

// mutators names the methods that change what a variable holds. A read-only method, such
// as a regular expression's MatchString, does not.
var mutators = map[string]bool{
	"Store": true, "Swap": true, "CompareAndSwap": true, "Add": true,
	"Lock": true, "Unlock": true, "RLock": true, "RUnlock": true,
	"LoadOrStore": true, "Delete": true,
}

// finding is one package-level variable that holds state.
type finding struct {
	pos  string
	name string
	why  string
}

// String renders one finding as "file:line: name: why", which an editor can jump to.
func (f finding) String() string {
	return fmt.Sprintf("%s: %s: %s", f.pos, f.name, f.why)
}

// main reports every package-level variable that holds state, and exits 1 when it finds
// one.
func main() {
	dir := flag.String("dir", "", "the directory to scan, which defaults to the workspace root")
	flag.Parse()

	root := *dir
	if root == "" {
		// The command runs from the tools directory, so the workspace root is the same
		// tree that holds every module.
		wd, err := os.Getwd()
		if err != nil {
			fail(err)
		}
		root, err = workspace.FindRoot(wd)
		if err != nil {
			fail(err)
		}
	}

	findings, err := scan(root, allowed)
	if err != nil {
		fail(err)
	}
	for _, f := range findings {
		fmt.Println(f)
	}
	if len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "pkgstate: %d package-level variable(s) hold state, and only %d is allowed\n",
			len(findings), len(allowed))
		os.Exit(1)
	}
}

// fail prints an error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "pkgstate:", err)
	os.Exit(1)
}

// scan walks every Go package under root and returns one finding per package-level
// variable that holds state, leaving out the names in allow.
func scan(root string, allow map[string]bool) ([]finding, error) {
	files, err := goFiles(root)
	if err != nil {
		return nil, err
	}
	// A package is one directory, because a write in one file of a package may name a
	// variable another file declares.
	byDir := map[string][]string{}
	for _, path := range files {
		byDir[filepath.Dir(path)] = append(byDir[filepath.Dir(path)], path)
	}

	var findings []finding
	for _, dir := range sortedKeys(byDir) {
		found, err := scanPackage(dir, byDir[dir], allow)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	return findings, nil
}

// scanPackage parses one directory and reports the package-level variables of it that
// hold state.
func scanPackage(dir string, paths []string, allow map[string]bool) ([]finding, error) {
	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(paths))
	for _, path := range paths {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, file)
	}

	// stateful holds the package-level variables of this directory, by name.
	stateful := map[string]finding{}
	for _, file := range parsed {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range value.Names {
					if allow[name.Name] || name.Name == "_" {
						continue
					}
					if isLockOrAtomic(value.Type) {
						stateful[name.Name] = finding{
							pos: fset.Position(name.Pos()).String(), name: name.Name,
							why: "the type is a lock or an atomic",
						}
						continue
					}
					stateful[name.Name] = finding{
						pos: fset.Position(name.Pos()).String(), name: name.Name,
						why: "some code writes it",
					}
				}
			}
		}
	}
	if len(stateful) == 0 {
		return nil, nil
	}

	// A write is an assignment to the name, or a method call on it, because a method may
	// change what the variable holds. A read-only table never sees either one.
	written := map[string]bool{}
	for _, file := range parsed {
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.AssignStmt:
				for _, left := range n.Lhs {
					markWrite(left, stateful, written)
				}
			case *ast.IncDecStmt:
				markWrite(n.X, stateful, written)
			case *ast.CallExpr:
				if selector, ok := n.Fun.(*ast.SelectorExpr); ok && mutators[selector.Sel.Name] {
					markWrite(selector.X, stateful, written)
				}
			}
			return true
		})
	}

	findings := make([]finding, 0, len(written))
	for name := range written {
		findings = append(findings, stateful[name])
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].pos < findings[j].pos })
	return findings, nil
}

// markWrite records that one expression writes a package-level variable, when the
// expression names one.
//
// ponytail: the name is matched by identifier, so a local variable that shadows a
// package-level name of the same package counts as a write too, and a write from another
// package or through a pointer is not seen. Track scopes and types if that ceiling
// matters.
func markWrite(expr ast.Expr, stateful map[string]finding, written map[string]bool) {
	ident := rootIdent(expr)
	if ident == nil {
		return
	}
	if _, isState := stateful[ident.Name]; isState {
		written[ident.Name] = true
	}
}

// rootIdent returns the identifier a write reaches through, whether it names the
// variable itself or a field or an element of it.
func rootIdent(expr ast.Expr) *ast.Ident {
	switch node := expr.(type) {
	case *ast.Ident:
		return node
	case *ast.SelectorExpr:
		return rootIdent(node.X)
	case *ast.IndexExpr:
		return rootIdent(node.X)
	case *ast.IndexListExpr:
		return rootIdent(node.X)
	case *ast.StarExpr:
		return rootIdent(node.X)
	case *ast.ParenExpr:
		return rootIdent(node.X)
	}
	return nil
}

// isLockOrAtomic reports whether a declared type is a lock or an atomic, which holds
// state even before any code writes it.
func isLockOrAtomic(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "sync" || pkg.Name == "atomic"
}

// goFiles returns every non-test Go file under root, sorted, so a run reads the same
// files in the same order.
func goFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

// sortedKeys returns the keys of a map, sorted.
func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
