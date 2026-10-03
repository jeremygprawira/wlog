// Package init implements `wlog init`: it detects the HTTP framework in a Go module and
// writes a compiling wlog setup. Nothing is written until the whole plan succeeds, so a
// failure halfway leaves the tree as it was.
package init

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	"github.com/jeremygprawira/wlog/cmd/wlog/internal/adapters"
)

// Options holds one init run's settings.
type Options struct {
	Dir       string
	Framework string // auto, nethttp, mux, echo, echo5, gin, chi, fiber, fiber3
	Drain     string // stdout, axiom, loki, file
	DryRun    bool
	Yes       bool // accept the whole plan and write it
	JSON      bool // print the plan as JSON
}

// Run parses args and runs init, returning the process exit code. Exit 0 on success,
// 1 when the setup file already exists or a step fails, and 2 on a usage or detection error.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	opts := Options{}
	flags.StringVar(&opts.Framework, "framework", "auto", "auto, nethttp, mux, echo, echo5, gin, chi, fiber, or fiber3")
	flags.StringVar(&opts.Drain, "drain", "stdout", "stdout, axiom, loki, or file")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "print the plan and write nothing")
	flags.BoolVar(&opts.Yes, "yes", false, "accept the whole plan and write it")
	flags.BoolVar(&opts.JSON, "json", false, "print the plan as JSON")
	flags.StringVar(&opts.Dir, "dir", ".", "the module directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if !knownDrain(opts.Drain) {
		_, _ = fmt.Fprintf(stderr, "wlog init: unknown drain %q: use stdout, axiom, loki, or file\n", opts.Drain)
		return 2
	}
	if err := validateGlobs(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "wlog init:", err)
		return 2
	}
	return run(opts, stdout, stderr)
}

// run does the work, so a test can call it directly.
func run(opts Options, stdout, stderr io.Writer) int {
	document, files, err := buildPlan(opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "wlog init:", err)
		return 1
	}
	if !opts.JSON {
		for _, adapter := range document.Adapters {
			if adapter.Kind == "http" || adapter.Setup == "" {
				continue
			}
			_, _ = fmt.Fprintf(stdout, "%s: %s\n", adapter.Name, adapter.Setup)
		}
	}
	if opts.JSON {
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "wlog init:", err)
			return 1
		}
		_, _ = fmt.Fprintln(stdout, string(data))
	}
	if opts.DryRun || !opts.Yes {
		if !opts.JSON {
			for _, write := range files {
				_, _ = fmt.Fprint(stdout, unifiedDiff(write.path, write.content))
			}
		}
		if !opts.Yes {
			_, _ = fmt.Fprintln(stderr, "wlog init: pass --yes to write these files")
		}
		return 0
	}
	before, err := snapshot(files)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "wlog init:", err)
		return 1
	}
	if err := apply(files); err != nil {
		restore(before)
		_, _ = fmt.Fprintln(stderr, "wlog init:", err)
		return 1
	}
	for _, write := range files {
		_, _ = fmt.Fprintln(stdout, "wrote", write.path)
	}
	if code := verify(opts.Dir, stdout, stderr); code != 0 {
		restore(before)
		return code
	}
	return 0
}

// verify builds the module and runs doctor over it, and prints both results. A build failure
// or a failed check returns 1, because the tool wrote code that does not work.
func verify(dir string, stdout, stderr io.Writer) int {
	// The deadline bounds a build that hangs on a lock or a network fetch, so the tool never
	// waits forever.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", os.DevNull, "./...")
	build.Dir = dir
	build.Env = os.Environ()
	if output, err := build.CombinedOutput(); err != nil {
		_, _ = fmt.Fprintf(stderr, "wlog init: go build failed: %v\n%s", err, output)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "go build ./... ok")
	code := wlogdoctor.Run([]string{"--dir", dir}, stdout, stderr)
	if code != 0 {
		_, _ = fmt.Fprintln(stderr, "wlog init: wlog doctor reported a failure")
		return 1
	}
	return 0
}

// plan is the whole run as data: the module, the framework, every adapter it found, and the
// files it would write. `--json` prints it, so a script reads the same plan a reader sees.
type plan struct {
	Version   int           `json:"version"`
	Dir       string        `json:"dir"`
	Module    string        `json:"module"`
	Framework string        `json:"framework"`
	Adapters  []planAdapter `json:"adapters"`
	Files     []planFile    `json:"files"`
}

// planAdapter is one adapter the module uses.
type planAdapter struct {
	Name  string `json:"name"`
	Wlog  string `json:"wlog"`
	Kind  string `json:"kind"`
	Setup string `json:"setup"`
}

// planFile is one file the run writes.
type planFile struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

// write is one planned file change.
type write struct {
	path    string
	content string
}

// saved is one file as it was before apply.
type saved struct {
	path    string
	data    []byte
	existed bool
}

// snapshot reads every file the plan will write, so a failed verify can put them back.
func snapshot(files []write) ([]saved, error) {
	out := make([]saved, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file.path)
		if err != nil {
			if os.IsNotExist(err) {
				out = append(out, saved{path: file.path})
				continue
			}
			return nil, err
		}
		out = append(out, saved{path: file.path, data: data, existed: true})
	}
	return out, nil
}

// restore puts the tree back. A file the plan created is removed.
func restore(files []saved) {
	for i := len(files) - 1; i >= 0; i-- {
		file := files[i]
		if !file.existed {
			_ = os.Remove(file.path)
			continue
		}
		_ = os.WriteFile(file.path, file.data, 0o644)
	}
}

// plannedModule is a go.mod or go.sum change the run will write.
type plannedModule struct {
	write write
	plan  planFile
}

// requirementFiles runs go mod tidy on a copy, so the requirement change is a plan step
// and the later build does not edit the module itself.
func requirementFiles(modDir string, files []write) ([]plannedModule, error) {
	tmp, err := os.MkdirTemp("", "wlog-init-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := copyModule(modDir, tmp); err != nil {
		return nil, err
	}
	for _, file := range files {
		rel, err := filepath.Rel(modDir, file.path)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		dest := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, []byte(file.content), 0o644); err != nil {
			return nil, err
		}
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = tmp
	tidy.Env = os.Environ()
	if output, err := tidy.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("go mod tidy: %w\n%s", err, output)
	}
	var extra []plannedModule
	for _, name := range []string{"go.mod", "go.sum"} {
		updated, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		target := filepath.Join(modDir, name)
		current, err := os.ReadFile(target)
		action := "update"
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			action = "create"
			current = nil
		}
		if string(current) == string(updated) {
			continue
		}
		extra = append(extra, plannedModule{
			write: write{path: target, content: string(updated)},
			plan:  planFile{Path: target, Action: action},
		})
	}
	return extra, nil
}

// copyModule copies the module tree. It skips directories the tidy does not read.
func copyModule(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			base := entry.Name()
			if base == ".git" || base == "vendor" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
}

// apply writes every planned file. The plan is complete before this runs, and each file lands
// whole: the bytes go to a temporary file beside the target and are renamed over it, so a reader
// never sees half a file and a failure leaves the old one in place.
func apply(plan []write) error {
	for _, write := range plan {
		if err := writeAtomic(write.path, []byte(write.content)); err != nil {
			return err
		}
	}
	return nil
}

// writeAtomic writes one file through a temporary file in the same directory. A rename within one
// directory is atomic on every platform this runs on.
func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

// knownDrain reports whether a drain name is one the tool can write.
func knownDrain(name string) bool {
	switch name {
	case "", "stdout", "axiom", "loki", "file":
		return true
	default:
		return false
	}
}

// moduleDir walks up from dir until it finds a go.mod.
func moduleDir(dir string) (string, error) {
	current, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("read go.mod: no go.mod at or above %s", dir)
		}
		current = parent
	}
}

// moduleName reads the module path from go.mod.
func moduleName(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("go.mod has no module line")
}

// detectFramework returns the HTTP framework the module imports, or "" when it finds none. The
// order matters: echo v5 comes before echo v4, because the v5 import path holds the v4 prefix.
func detectFramework(sources [][]byte) string {
	found := ""
	for _, file := range sources {
		source := string(file)
		switch {
		case strings.Contains(source, "github.com/labstack/echo/v5"):
			return "echo5"
		case strings.Contains(source, "github.com/labstack/echo/v4"):
			return "echo"
		case strings.Contains(source, "github.com/gin-gonic/gin"):
			return "gin"
		case strings.Contains(source, "github.com/go-chi/chi/v5"):
			return "chi"
		case strings.Contains(source, "github.com/gofiber/fiber/v3"):
			return "fiber3"
		case strings.Contains(source, "github.com/gofiber/fiber/v2"):
			return "fiber"
		case strings.Contains(source, "github.com/gorilla/mux"):
			if found == "" {
				found = "mux"
			}
		case strings.Contains(source, `"net/http"`) && found == "":
			found = "nethttp"
		}
	}
	return found
}

// packageName reads the package clause from the module's first Go file.
func packageName(dir string) (string, error) {
	paths, err := listGoFiles(dir)
	if err != nil {
		return "", err
	}
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(string(source), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "package "); ok {
				return strings.TrimSpace(rest), nil
			}
		}
	}
	return "", fmt.Errorf("no package clause found in %s", dir)
}

// listGoFiles returns the app's Go files. A _test.go file is not app source.
func listGoFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths, nil
}

// goFiles returns the source of every app Go file in dir.
func goFiles(dir string) ([][]byte, error) {
	paths, err := listGoFiles(dir)
	if err != nil {
		return nil, err
	}
	var sources [][]byte
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no .go files in %s", dir)
	}
	return sources, nil
}

// buildPlan returns the plan document and every file change, or an error before anything is
// written. It reads go.mod and the module's imports, so the plan names every adapter the module
// uses before the tool touches a file.
func buildPlan(opts Options) (plan, []write, error) {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	document := plan{Version: 1, Dir: dir}
	setupPath := filepath.Join(dir, "wlog_setup.go")
	if _, err := os.Stat(setupPath); err == nil {
		return document, nil, fmt.Errorf("%s already exists", setupPath)
	}
	legacy := filepath.Join(dir, "wlog.go")
	if _, err := os.Stat(legacy); err == nil {
		return document, nil, fmt.Errorf("%s already exists", legacy)
	}
	modDir, err := moduleDir(dir)
	if err != nil {
		return document, nil, err
	}
	module, err := moduleName(modDir)
	if err != nil {
		return document, nil, err
	}
	document.Module = module
	goMod, err := os.ReadFile(filepath.Join(modDir, "go.mod"))
	if err != nil {
		return document, nil, fmt.Errorf("read go.mod: %w", err)
	}
	sources, err := goFiles(dir)
	if err != nil {
		return document, nil, err
	}
	document.Adapters = detectAdapters(string(goMod), sources)
	framework := opts.Framework
	if framework == "" || framework == "auto" {
		framework = detectFramework(sources)
	}
	document.Framework = framework
	pkg, err := packageName(dir)
	if err != nil {
		return document, nil, err
	}
	if err := namesTaken(dir, generatedNames(framework)); err != nil {
		return document, nil, err
	}
	setup, err := setupFor(framework, opts.Drain, module, pkg)
	if err != nil {
		return document, nil, err
	}

	files := []write{
		{path: setupPath, content: setup.wlogGo},
		{path: filepath.Join(dir, ".env.example"), content: mergeEnvExample(filepath.Join(dir, ".env.example"), setup.envExample)},
	}
	document.Files = []planFile{
		{Path: setupPath, Action: "create"},
		{Path: filepath.Join(dir, ".env.example"), Action: "update"},
	}
	routerPath := ""
	if framework != "" {
		var patched string
		routerPath, patched, err = patchRouter(dir, framework)
		if err != nil {
			return document, nil, err
		}
		if routerPath != "" {
			files = append(files, write{path: routerPath, content: patched})
			document.Files = append(document.Files, planFile{Path: routerPath, Action: "update"})
		}
	}
	extra, err := requirementFiles(modDir, files)
	if err != nil {
		return document, nil, err
	}
	for _, file := range extra {
		files = append(files, file.write)
		document.Files = append(document.Files, file.plan)
	}
	return document, files, nil
}

// detectAdapters returns every adapter the module uses, sorted by name. A third-party module in
// go.mod implies its adapter, and an import of an adapter package implies it too. The table is
// sorted by name, so the result is too.
func detectAdapters(goMod string, sources [][]byte) []planAdapter {
	required := requiredModules(goMod)
	imported := map[string]bool{}
	for _, adapter := range adapters.Table {
		for _, source := range sources {
			if strings.Contains(string(source), `"`+adapter.Wlog+`"`) {
				imported[adapter.Wlog] = true
			}
		}
	}
	found := []planAdapter{}
	for _, adapter := range adapters.Table {
		installed := imported[adapter.Wlog]
		for _, lib := range adapter.Libs {
			if required[lib] {
				installed = true
			}
		}
		if !installed {
			continue
		}
		found = append(found, planAdapter{
			Name: adapter.Name, Wlog: adapter.Wlog, Kind: adapter.Kind,
			Setup: adapter.Setup,
		})
	}
	return found
}

// requiredModules reads the module paths a go.mod requires, with the comment and the version
// removed. An indirect line does not count, and neither does a replace or exclude block.
// The wlog modules are skipped, because those are the adapters the tool writes, not
// the libraries that imply one.
func requiredModules(goMod string) map[string]bool {
	required := map[string]bool{}
	skipBlock := false
	for _, raw := range strings.Split(goMod, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "replace (") || strings.HasPrefix(line, "exclude (") {
			skipBlock = true
			continue
		}
		if line == ")" {
			skipBlock = false
			continue
		}
		if skipBlock || strings.Contains(raw, "// indirect") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "require "); ok {
			line = strings.TrimSpace(rest)
		}
		if line == "" || strings.HasPrefix(line, "//") ||
			strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "go ") ||
			strings.HasPrefix(line, "replace ") || strings.HasPrefix(line, "exclude ") ||
			strings.HasPrefix(line, "retract ") || strings.HasPrefix(line, "toolchain ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.HasPrefix(fields[0], "github.com/jeremygprawira/wlog") {
			continue
		}
		required[fields[0]] = true
	}
	return required
}

// patchRouter installs the middleware in the file that declares the server or the router. It
// returns "" when no file needs a change.
func patchRouter(dir, framework string) (string, string, error) {
	paths, err := listGoFiles(dir)
	if err != nil {
		return "", "", err
	}
	for _, path := range paths {
		if filepath.Base(path) == "wlog_setup.go" {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		patched, changed, err := rewrite(path, source, framework)
		if err != nil {
			return "", "", err
		}
		if changed {
			return path, string(patched), nil
		}
		if routerAlreadyUsesWlog(path, source, framework) {
			return "", "", nil
		}
	}
	return "", "", fmt.Errorf("no router declaration found for %s", framework)
}

// routerAlreadyUsesWlog reports whether this file builds the router and already calls wlog.
func routerAlreadyUsesWlog(filename string, source []byte, framework string) bool {
	if framework == "nethttp" || framework == "mux" || framework == "" {
		return false
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, parser.ParseComments)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for _, statement := range block.List {
			router, ok := routerName(statement, framework)
			if ok && hasWlogMiddleware(file, block.List, router) {
				found = true
			}
		}
		return true
	})
	return found
}

// generatedNames lists the functions the setup file will declare.
func generatedNames(framework string) []string {
	names := []string{"NewLogger"}
	switch framework {
	case "nethttp", "mux":
		names = append(names, "WrapHandler")
	case "":
	default:
		names = append(names, "LoggerMiddleware")
	}
	return names
}

// namesTaken reports a function the app already declares that the setup would add.
func namesTaken(dir string, names []string) error {
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	paths, err := listGoFiles(dir)
	if err != nil {
		return err
	}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil || !want[fn.Name.Name] {
				continue
			}
			return fmt.Errorf("%s already declares %s", path, fn.Name.Name)
		}
	}
	return nil
}

// unifiedDiff renders one planned file as a unified diff, the format a reader expects from a tool
// that says what it would change.
func unifiedDiff(path, content string) string {
	old, err := os.ReadFile(path)
	if err != nil {
		old = nil
	}
	oldLines := splitLines(string(old))
	newLines := splitLines(content)
	var builder strings.Builder
	fmt.Fprintf(&builder, "--- %s\n+++ %s\n", path, path)
	fmt.Fprintf(&builder, "@@ -%d,%d +%d,%d @@\n", 1, len(oldLines), 1, len(newLines))
	for _, line := range oldLines {
		fmt.Fprintf(&builder, "-%s\n", line)
	}
	for _, line := range newLines {
		fmt.Fprintf(&builder, "+%s\n", line)
	}
	return builder.String()
}

// splitLines splits a file into lines without their terminator, dropping the empty tail a final
// newline leaves.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// validateGlobs checks the run's option values that can be wrong before anything is written.
func validateGlobs(Options) error { return nil }

// mergeEnvExample adds the keys the setup needs to the file that is already there.
//
// A caller's file is theirs: its comments stay, its order stays, and a value it already sets is
// never replaced. Only a key the file does not mention is appended.
func mergeEnvExample(path, wanted string) string {
	existing, err := os.ReadFile(path)
	if err != nil {
		return wanted
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		if key, _, ok := strings.Cut(line, "="); ok {
			have[strings.TrimSpace(key)] = true
		}
	}

	var missing []string
	for _, line := range strings.Split(wanted, "\n") {
		key, _, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || have[key] {
			continue
		}
		missing = append(missing, line)
	}
	if len(missing) == 0 {
		return string(existing)
	}

	merged := strings.TrimRight(string(existing), "\n")
	return merged + "\n" + strings.Join(missing, "\n") + "\n"
}

// setup is the generated code for one framework and drain.
type setup struct {
	wlogGo     string
	envExample string
}
