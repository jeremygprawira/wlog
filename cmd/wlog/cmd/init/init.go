// Package init implements `wlog init`: it detects the HTTP framework in a Go module and
// writes a compiling wlog setup. Nothing is written until the whole plan succeeds, so a
// failure halfway leaves the tree as it was.
package init

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Options holds one init run's settings.
type Options struct {
	Dir       string
	Framework string // auto, nethttp, mux, echo, echo5, gin
	Drain     string // stdout, axiom, loki, file
	DryRun    bool
}

// Run parses args and runs init, returning the process exit code. Exit 0 on success,
// 1 when wlog.go already exists, and 2 on a usage or detection error.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	opts := Options{}
	flags.StringVar(&opts.Framework, "framework", "auto", "auto, nethttp, mux, echo, echo5, or gin")
	flags.StringVar(&opts.Drain, "drain", "stdout", "stdout, axiom, loki, or file")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "print the plan and write nothing")
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
	plan, err := buildPlan(opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "wlog init:", err)
		return 1
	}
	if opts.DryRun {
		for _, write := range plan {
			_, _ = fmt.Fprint(stdout, unifiedDiff(write.path, write.content))
		}
		return 0
	}
	if err := apply(plan); err != nil {
		_, _ = fmt.Fprintln(stderr, "wlog init:", err)
		return 1
	}
	for _, write := range plan {
		_, _ = fmt.Fprintln(stdout, "wrote", write.path)
	}
	return 0
}

// write is one planned file change.
type write struct {
	path    string
	content string
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

// detectFramework reads the module's Go files and returns the framework it uses.
func detectFramework(dir string) (string, error) {
	files, err := goFiles(dir)
	if err != nil {
		return "", err
	}
	found := ""
	for _, file := range files {
		source := string(file)
		switch {
		case strings.Contains(source, "github.com/labstack/echo/v5"):
			return "echo5", nil
		case strings.Contains(source, "github.com/labstack/echo/v4"):
			return "echo", nil
		case strings.Contains(source, "github.com/gin-gonic/gin"):
			return "gin", nil
		case strings.Contains(source, "github.com/gorilla/mux"):
			found = "mux"
		case strings.Contains(source, `"net/http"`) && found == "":
			found = "nethttp"
		}
	}
	if found == "" {
		return "", fmt.Errorf("no HTTP framework found")
	}
	return found, nil
}

// packageName reads the package clause from the module's first Go file.
func packageName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, entry.Name()))
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

// goFiles returns the source of every .go file in dir.
func goFiles(dir string) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sources [][]byte
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, entry.Name()))
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

// buildPlan returns every file change, or an error before anything is written.
func buildPlan(opts Options) ([]write, error) {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	wlogPath := filepath.Join(dir, "wlog.go")
	if _, err := os.Stat(wlogPath); err == nil {
		return nil, fmt.Errorf("%s already exists", wlogPath)
	}
	module, err := moduleName(dir)
	if err != nil {
		return nil, err
	}
	framework := opts.Framework
	if framework == "" || framework == "auto" {
		framework, err = detectFramework(dir)
		if err != nil {
			return nil, err
		}
	}
	pkg, err := packageName(dir)
	if err != nil {
		return nil, err
	}
	setup, err := setupFor(framework, opts.Drain, module, pkg)
	if err != nil {
		return nil, err
	}

	plan := []write{
		{path: wlogPath, content: setup.wlogGo},
		{path: filepath.Join(dir, ".env.example"), content: mergeEnvExample(filepath.Join(dir, ".env.example"), setup.envExample)},
	}
	routerPath, patched, err := patchRouter(dir, framework)
	if err != nil {
		return nil, err
	}
	if routerPath != "" {
		plan = append(plan, write{path: routerPath, content: patched})
	}
	return plan, nil
}

// patchRouter installs the middleware in the file that declares the server or the router. It
// returns "" when no file needs a change.
func patchRouter(dir, framework string) (string, string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || entry.Name() == "wlog.go" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
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
	}
	return "", "", fmt.Errorf("no router declaration found for %s", framework)
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
