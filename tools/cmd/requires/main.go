// Command requires checks that every workspace module requires the sibling
// modules it imports, at the release version, with a local replace.
//
// The flow per module is: scan the module's Go files for imports, find the
// workspace module that owns each import path, then compare that with the
// require and replace lines of the module's go.mod. The command reports:
//
//	REQ1  a sibling module is imported but not required
//	REQ2  a require version differs from tools/version.txt
//	REQ3  a required sibling has no local replace, so GOWORK=off cannot build
//	REQ4  a module that a source file imports is marked // indirect
//
// Every problem prints as path:line: code: message, and the command exits 1.
package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// main finds the workspace root and exits 1 when any check fails.
func main() {
	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if err := run(root, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "requires:", err)
	os.Exit(1)
}

// problem is one finding, printed as path:line: code: message.
type problem struct {
	file string
	line int
	code string
	msg  string
}

// String prints the finding in the form a CI log can read.
func (p problem) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", p.file, p.line, p.code, p.msg)
}

// importSite is one import statement, with a path relative to the workspace
// root so a problem can name the exact line to fix.
type importSite struct {
	path string
	file string
	line int
}

// require is one line of a go.mod require directive or block.
type require struct {
	path     string
	version  string
	indirect bool
	line     int
}

// modFile holds the go.mod fields the check reads.
type modFile struct {
	requires map[string]require
	replaced map[string]bool
}

// run checks every workspace module and prints each problem to out.
//
// It returns an error when it finds at least one problem, so the exit code
// fails CI. A read error stops the run, because a half-read module could hide a
// problem.
func run(root string, out io.Writer) error {
	version, err := readVersion(filepath.Join(root, "tools", "version.txt"))
	if err != nil {
		return err
	}
	mods, err := workspace.Modules(root)
	if err != nil {
		return err
	}

	var probs []problem
	for _, m := range mods {
		dir := filepath.Join(root, m.Dir)
		mod, err := readModFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return err
		}
		imports, err := scanImports(dir, root, nestedModules(mods, m))
		if err != nil {
			return err
		}
		gomod := rel(root, filepath.Join(dir, "go.mod"))
		probs = append(probs, checkModule(m, mods, mod, imports, version, gomod)...)
	}

	sort.Slice(probs, func(i, j int) bool {
		if probs[i].file != probs[j].file {
			return probs[i].file < probs[j].file
		}
		return probs[i].line < probs[j].line
	})
	for _, p := range probs {
		if _, err := fmt.Fprintln(out, p); err != nil {
			return err
		}
	}
	if len(probs) > 0 {
		return fmt.Errorf("%d problem(s)", len(probs))
	}
	return nil
}

// checkModule returns every problem of one module.
//
// REQ4 holds for any module a source file imports, sibling or not. REQ1 to REQ3
// hold for sibling modules only, because version.txt speaks about this
// repository.
func checkModule(m workspace.Module, mods []workspace.Module, mod *modFile, imports []importSite, version, gomod string) []problem {
	var probs []problem

	for _, req := range sortedRequires(mod.requires) {
		if !req.indirect {
			continue
		}
		if directlyImported(mod.requires, imports)[req.path] {
			probs = append(probs, problem{
				file: gomod,
				line: req.line,
				code: "REQ4",
				msg:  fmt.Sprintf("%s is imported directly, so drop the // indirect marker", req.path),
			})
		}
	}

	// The first import site of each sibling names the file to fix.
	sites := map[string]importSite{}
	for _, imp := range imports {
		owner := ownerModule(mods, imp.path)
		if owner == nil || owner.Path == m.Path {
			continue
		}
		if _, ok := sites[owner.Path]; !ok {
			sites[owner.Path] = imp
		}
	}
	for _, path := range sortedKeys(sites) {
		imp := sites[path]
		req, ok := mod.requires[path]
		if !ok {
			probs = append(probs, problem{
				file: imp.file,
				line: imp.line,
				code: "REQ1",
				msg:  fmt.Sprintf("imports %s, so add require %s %s", imp.path, path, version),
			})
			continue
		}
		if req.version != version {
			probs = append(probs, problem{
				file: gomod,
				line: req.line,
				code: "REQ2",
				msg:  fmt.Sprintf("requires %s at %s, want %s from tools/version.txt", path, req.version, version),
			})
		}
		if !mod.replaced[path] {
			probs = append(probs, problem{
				file: gomod,
				line: req.line,
				code: "REQ3",
				msg:  fmt.Sprintf("requires %s with no replace, so GOWORK=off cannot find it", path),
			})
		}
	}
	return probs
}

// readVersion reads the release version that every sibling require must name.
func readVersion(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", fmt.Errorf("%s is empty", file)
	}
	return version, nil
}

// readModFile reads the require and replace lines of one go.mod file.
//
// It reads the single and the block form of both directives. A require line
// keeps its // indirect marker, because REQ4 depends on it. A replace line
// counts whatever its target is, since the check compares module paths only.
func readModFile(file string) (*modFile, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	mod := &modFile{requires: map[string]require{}, replaced: map[string]bool{}}

	inRequire, inReplace := false, false
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(comment(raw))
		switch {
		case line == "":
		case (inRequire || inReplace) && line == ")":
			inRequire, inReplace = false, false
		case inRequire:
			mod.addRequire(line, i+1, strings.Contains(raw, "// indirect"))
		case inReplace:
			mod.addReplace(line)
		case line == "require (":
			inRequire = true
		case line == "replace (":
			inReplace = true
		case strings.HasPrefix(line, "require "):
			mod.addRequire(strings.TrimSpace(line[len("require "):]), i+1, strings.Contains(raw, "// indirect"))
		case strings.HasPrefix(line, "replace "):
			mod.addReplace(strings.TrimSpace(line[len("replace "):]))
		}
	}
	return mod, nil
}

// addRequire adds one require line, for example "example.com/lib v0.1.0" or the
// same line with a trailing // indirect. indirect comes from the raw line,
// because the parser strips comments before it reads the fields.
func (m *modFile) addRequire(line string, number int, indirect bool) {
	fields := strings.Fields(comment(line))
	if len(fields) == 0 {
		return
	}
	req := require{path: fields[0], indirect: indirect, line: number}
	if len(fields) > 1 {
		req.version = fields[1]
	}
	m.requires[req.path] = req
}

// addReplace records the module path a replace line replaces.
func (m *modFile) addReplace(line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	m.replaced[fields[0]] = true
}

// sortedRequires returns the requires of a module, sorted by path.
func sortedRequires(reqs map[string]require) []require {
	out := make([]require, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// sortedKeys returns the keys of a map, sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ownerModule returns the workspace module that owns an import path, or nil.
//
// The longest matching module path wins, so a package of a sub-module never
// counts as a package of the root module.
func ownerModule(mods []workspace.Module, importPath string) *workspace.Module {
	var best *workspace.Module
	for i := range mods {
		path := mods[i].Path
		if importPath != path && !strings.HasPrefix(importPath, path+"/") {
			continue
		}
		if best == nil || len(path) > len(best.Path) {
			best = &mods[i]
		}
	}
	return best
}

// directlyImported returns the require paths that the imports make direct.
//
// The longest require that owns an import path wins, because a package of a
// sub-module never makes its parent module a direct dependency. The import
// "go.opentelemetry.io/otel/trace" makes the trace module direct, and it leaves
// the otel module indirect.
func directlyImported(reqs map[string]require, imports []importSite) map[string]bool {
	direct := map[string]bool{}
	for _, imp := range imports {
		best := ""
		for path := range reqs {
			if imp.path != path && !strings.HasPrefix(imp.path, path+"/") {
				continue
			}
			if len(path) > len(best) {
				best = path
			}
		}
		if best != "" {
			direct[best] = true
		}
	}
	return direct
}

// scanImports returns every import of the Go files under dir.
//
// It skips vendor, testdata, and hidden directories, and it skips every
// directory in nested, so a module never reports the imports of a module that
// lives inside it. Every file path is relative to root.
func scanImports(dir, root string, nested []string) ([]importSite, error) {
	var sites []importSite
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			if path != dir && nestedDir(rel(root, path), nested) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			sites = append(sites, importSite{
				path: value,
				file: rel(root, path),
				line: fset.Position(spec.Path.Pos()).Line,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sites, nil
}

// nestedDir reports whether a workspace dir holds another module.
//
// site is the "./x/y" form that rel returns, and dirs hold the dirs of the
// other modules of the workspace in the same form.
func nestedDir(site string, dirs []string) bool {
	for _, dir := range dirs {
		if site == dir || strings.HasPrefix(site, dir+"/") {
			return true
		}
	}
	return false
}

// nestedModules returns the dirs of every module except m, in the "./x" form.
//
// The scan of a module skips them, so the root module never reports the imports
// of a sub-module that lives in its tree.
func nestedModules(mods []workspace.Module, m workspace.Module) []string {
	var dirs []string
	for _, other := range mods {
		if other.Dir == m.Dir {
			continue
		}
		dir := strings.TrimPrefix(filepath.ToSlash(other.Dir), "./")
		if dir == "" {
			continue
		}
		dirs = append(dirs, "./"+dir)
	}
	return dirs
}

// comment drops a trailing // comment from a go.mod line.
func comment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// rel returns path as "./x/y" relative to root, and path as written when no
// relative path exists.
func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return "./" + filepath.ToSlash(r)
}
