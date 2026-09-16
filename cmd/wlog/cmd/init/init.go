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
	"regexp"
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
			_, _ = fmt.Fprintf(stdout, "--- %s\n%s\n", write.path, write.content)
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

// apply writes every planned file. The plan is complete before this runs.
func apply(plan []write) error {
	for _, write := range plan {
		if err := os.WriteFile(write.path, []byte(write.content), 0o644); err != nil {
			return err
		}
	}
	return nil
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
		{path: filepath.Join(dir, ".env.example"), content: setup.envExample},
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

// routerPatterns find the router construction in a source file.
var routerPatterns = map[string]*regexp.Regexp{
	"echo":    regexp.MustCompile(`(?m)^(\s*)(\w+)\s*:=\s*echo\.New\(\)`),
	"echo5":   regexp.MustCompile(`(?m)^(\s*)(\w+)\s*:=\s*echo\.New\(\)`),
	"gin":     regexp.MustCompile(`(?m)^(\s*)(\w+)\s*:=\s*gin\.(?:New|Default)\(\)`),
	"nethttp": regexp.MustCompile(`(?m)^(\s*)(\w+)\s*:=\s*http\.NewServeMux\(\)`),
	"mux":     regexp.MustCompile(`(?m)^(\s*)(\w+)\s*:=\s*mux\.NewRouter\(\)`),
}

// listenPattern finds the server start, where a net/http handler gets wrapped.
var listenPattern = regexp.MustCompile(`(http\.ListenAndServe\([^,]+,\s*)([^)]+)(\))`)

// patchRouter inserts one middleware line, or wraps the server handler, in the file
// that declares the router. It returns "" when no file needs a change.
func patchRouter(dir, framework string) (string, string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", err
	}
	pattern := routerPatterns[framework]
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || entry.Name() == "wlog.go" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		text := string(source)
		if framework == "nethttp" || framework == "mux" {
			if !listenPattern.MatchString(text) {
				continue
			}
			patched := listenPattern.ReplaceAllString(text, "${1}WrapHandler(${2})${3}")
			return path, patched, nil
		}
		match := pattern.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		indent, router := match[1], match[2]
		insert := match[0] + "\n" + indent + router + ".Use(LoggerMiddleware())"
		return path, strings.Replace(text, match[0], insert, 1), nil
	}
	return "", "", fmt.Errorf("no router declaration found for %s", framework)
}

// setup is the generated code for one framework and drain.
type setup struct {
	wlogGo     string
	envExample string
}
