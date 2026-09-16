// Command fuzz runs every Fuzz target of every module for a fixed time.
//
// A `go test -fuzz` command names one target, so a repository that adds a target
// leaves it unwatched until someone remembers to edit the Makefile. The command
// finds the targets itself, by reading the test files of each module, and runs
// each one for the time that -time gives.
//
// A crash stays in testdata/fuzz/ of its package, which git tracks, so the input
// that broke the code arrives with the fix.
package main

import (
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// main finds the workspace root, then runs every fuzz target.
func main() {
	duration := flag.String("time", "30s", "how long each target runs")
	only := flag.String("only", "", "run only the targets whose name holds this string")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if err := run(root, *duration, *only, runTarget, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "fuzz:", err)
	os.Exit(1)
}

// target is one fuzz function, with the package that holds it.
type target struct {
	module  string // the module dir, as go.work writes it
	pkg     string // the package dir relative to the module, such as "./redact"
	name    string // the function name, such as FuzzRedact_NeverLeaks
	relPath string // the file path relative to the root, for a report
}

// run finds every fuzz target and runs each one.
//
// runTarget is a parameter, so a test proves the discovery and the report
// without a fuzz run.
func run(root, duration, only string, runTarget func(dir, pkg, name, duration string) ([]byte, error), out io.Writer) error {
	targets, err := find(root)
	if err != nil {
		return err
	}
	if only != "" {
		kept := targets[:0]
		for _, t := range targets {
			if strings.Contains(t.name, only) {
				kept = append(kept, t)
			}
		}
		targets = kept
	}
	if len(targets) == 0 {
		return fmt.Errorf("no Fuzz target found")
	}

	for _, t := range targets {
		dir := filepath.Join(root, t.module)
		if _, err := fmt.Fprintf(out, "fuzz %s in %s for %s\n", t.name, t.pkg, duration); err != nil {
			return err
		}
		if text, err := runTarget(dir, t.pkg, t.name, duration); err != nil {
			if _, err := fmt.Fprintf(out, "%s: FUZZ1: %v\n%s", t.relPath, err, indent(text)); err != nil {
				return err
			}
			return fmt.Errorf("%s failed", t.name)
		}
	}
	return nil
}

// find returns every Fuzz target of every module, sorted by module and name.
//
// It reads the test files with the Go parser rather than searching for text, so
// a mention of a target inside a comment never counts.
func find(root string) ([]target, error) {
	mods, err := workspace.Modules(root)
	if err != nil {
		return nil, err
	}

	// The root module holds every sub-module in its tree, so its walk must skip
	// a directory that another module owns. Otherwise a target is found twice:
	// once under the wrong module, and once under its own.
	others := map[string]bool{}
	for _, m := range mods {
		if m.Dir != "." {
			others[strings.TrimPrefix(filepath.ToSlash(m.Dir), "./")] = true
		}
	}

	var targets []target
	for _, m := range mods {
		dir := filepath.Join(root, m.Dir)
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if path != dir && (name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".")) {
					return fs.SkipDir
				}
				if path != dir && others[filepath.ToSlash(mustRel(root, path))] {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			// The package directory in the form that go test accepts: "." for
			// the root package of a module, "./x" for a sub-directory.
			pkgDir := "."
			if rel := mustRel(dir, filepath.Dir(path)); rel != "." {
				pkgDir = "./" + filepath.ToSlash(rel)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || !strings.HasPrefix(fn.Name.Name, "Fuzz") {
					continue
				}
				if len(fn.Type.Params.List) != 1 {
					continue
				}
				targets = append(targets, target{
					module:  m.Dir,
					pkg:     pkgDir,
					name:    fn.Name.Name,
					relPath: rel(root, path),
				})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].module != targets[j].module {
			return targets[i].module < targets[j].module
		}
		return targets[i].name < targets[j].name
	})
	return targets, nil
}

// runTarget runs one fuzz target for the given time.
func runTarget(dir, pkg, name, duration string) ([]byte, error) {
	// One worker: a target that captures process stdout (the sink tests do)
	// would otherwise read another worker's output and report a false leak.
	cmd := exec.CommandContext(context.Background(), "go", "test",
		"-run=xxx", "-fuzz="+name, "-fuzztime="+duration, "-parallel=1", pkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	return cmd.CombinedOutput()
}

// indent puts four spaces before every line of a fuzz report.
func indent(text []byte) string {
	lines := strings.Split(strings.TrimRight(string(text), "\n"), "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

// rel returns a path relative to the root, with a leading "./".
func rel(root, path string) string {
	return "./" + filepath.ToSlash(mustRel(root, path))
}

// mustRel returns a path relative to the base, or the path itself.
func mustRel(base, path string) string {
	r, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return r
}
