package init_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// v2Plan is the shape `wlog init --json` prints. The test decodes it, so a field rename is a
// test failure rather than a silent change to the contract.
type v2Plan struct {
	Version   int    `json:"version"`
	Dir       string `json:"dir"`
	Module    string `json:"module"`
	Framework string `json:"framework"`
	Adapters  []struct {
		Name      string `json:"name"`
		Wlog      string `json:"wlog"`
		Setup     string `json:"setup"`
		Installed bool   `json:"installed"`
	} `json:"adapters"`
	Files []struct {
		Path   string `json:"path"`
		Action string `json:"action"`
	} `json:"files"`
}

// chiTree copies the chi fixture and writes a go.mod that requires chi and the wlog chi
// adapter, so the tree builds outside the workspace.
func chiTree(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	source := []byte(`package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {})
	_ = http.ListenAndServe(":8080", r)
}
`)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	goMod := "module example.com/chi-app\n\ngo " + moduleFloor(t, filepath.Join("..", "..", "go.mod")) + `

require (
	github.com/go-chi/chi/v5 v5.3.2
	github.com/jeremygprawira/wlog v0.0.0
	github.com/jeremygprawira/wlog/middleware/chi v0.0.0
)

replace github.com/jeremygprawira/wlog => ` + root + `

replace github.com/jeremygprawira/wlog/middleware/chi => ` + filepath.Join(root, "middleware", "chi") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// recipeTree copies one recipe's main.go into a temp module that requires the modules the
// recipe imports, so `wlog init` and `wlog doctor` run against a standalone app.
func recipeTree(t *testing.T, name string, requires []string, replaces map[string]string) string {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "examples", name, "main.go"))
	if err != nil {
		t.Fatalf("read recipe %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	var builder strings.Builder
	builder.WriteString("module example.com/" + name + "\n\ngo 1.25.0\n\nrequire (\n")
	builder.WriteString("\tgithub.com/jeremygprawira/wlog v0.0.0\n")
	for _, require := range requires {
		builder.WriteString("\t" + require + "\n")
	}
	builder.WriteString(")\n\nreplace github.com/jeremygprawira/wlog => " + root + "\n")
	for module, path := range replaces {
		builder.WriteString("replace " + module + " => " + filepath.Join(root, filepath.FromSlash(path)) + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// buildV2 builds every package in a generated tree.
func buildV2(t *testing.T, dir string) {
	t.Helper()
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the generated tree does not build: %v\n%s", err, output)
	}
}

// TestInitV2_JSONPlanNamesEveryAdapter proves `--json` prints the module, the framework, each
// detected adapter with its install line, and the files the run would write.
func TestInitV2_JSONPlanNamesEveryAdapter(t *testing.T) {
	dir := chiTree(t)
	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--json", "--dry-run"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("init exit %d\n%s", code, stdout.String())
	}
	var plan v2Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, stdout.String())
	}
	if plan.Module != "example.com/chi-app" {
		t.Errorf("module = %q", plan.Module)
	}
	if plan.Framework != "chi" {
		t.Errorf("framework = %q, want chi", plan.Framework)
	}
	var found bool
	for _, adapter := range plan.Adapters {
		if adapter.Name == "http-chi" {
			found = true
			if adapter.Wlog != "github.com/jeremygprawira/wlog/middleware/chi" {
				t.Errorf("wlog = %q", adapter.Wlog)
			}
			if adapter.Setup == "" {
				t.Error("http-chi has no install line")
			}
			if !adapter.Installed {
				t.Error("http-chi is imported but reported not installed")
			}
		}
	}
	if !found {
		t.Errorf("the plan names no http-chi adapter: %+v", plan.Adapters)
	}
	var wroteSetup bool
	for _, file := range plan.Files {
		if strings.HasSuffix(file.Path, "wlog_setup.go") {
			wroteSetup = true
		}
	}
	if !wroteSetup {
		t.Errorf("the plan writes no wlog_setup.go: %+v", plan.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog_setup.go")); !os.IsNotExist(err) {
		t.Error("--dry-run wrote wlog_setup.go")
	}
}

// TestInitV2_YesWritesAndBuilds proves `--yes` writes the setup, the entry point calls the
// generated middleware, and the tree builds.
func TestInitV2_YesWritesAndBuilds(t *testing.T) {
	dir := chiTree(t)
	if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("init exit %d", code)
	}
	setup := readFile(t, filepath.Join(dir, "wlog_setup.go"))
	if !strings.Contains(setup, "setup.FromEnv()") {
		t.Errorf("wlog_setup.go does not build the logger from the environment:\n%s", setup)
	}
	main := readFile(t, filepath.Join(dir, "main.go"))
	if !strings.Contains(main, "LoggerMiddleware") {
		t.Errorf("the entry point does not install the middleware:\n%s", main)
	}
	buildV2(t, dir)
	// A generated file that is not gofmt-clean fails the user's CI, so the tool writes one that
	// is.
	unformatted := exec.Command("gofmt", "-l", ".")
	unformatted.Dir = dir
	output, err := unformatted.CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt: %v\n%s", err, output)
	}
	if len(bytes.TrimSpace(output)) != 0 {
		t.Errorf("init wrote a file gofmt would change: %s", output)
	}
}

// TestInitV2_RecipesBuildAndPassDoctor proves criterion 6: on the llm-agent and mcp-server
// recipe apps, `init --yes` writes code that builds and passes `wlog doctor`.
func TestInitV2_RecipesBuildAndPassDoctor(t *testing.T) {
	recipes := []struct {
		name     string
		requires []string
		replaces map[string]string
	}{
		{
			name:     "llm-agent",
			requires: []string{"github.com/anthropics/anthropic-sdk-go v1.73.0", "github.com/jeremygprawira/wlog/ai/anthropic v0.0.0"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/ai/anthropic": "ai/anthropic"},
		},
		{
			name:     "mcp-server",
			requires: []string{"github.com/modelcontextprotocol/go-sdk v1.8.0", "github.com/jeremygprawira/wlog/ai/mcpsdk v0.0.0"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/ai/mcpsdk": "ai/mcpsdk"},
		},
	}
	for _, recipe := range recipes {
		t.Run(recipe.name, func(t *testing.T) {
			dir := recipeTree(t, recipe.name, recipe.requires, recipe.replaces)
			if code := wloginit.Run([]string{"--dir", dir, "--yes"}, io.Discard, io.Discard); code != 0 {
				t.Fatalf("init exit %d", code)
			}
			if _, err := os.Stat(filepath.Join(dir, "wlog_setup.go")); err != nil {
				t.Fatalf("wlog_setup.go missing: %v", err)
			}
			buildV2(t, dir)
			if code := wlogdoctor.Run([]string{"--dir", dir}, io.Discard, io.Discard); code != 0 {
				t.Errorf("wlog doctor exited %d on the generated recipe", code)
			}
		})
	}
}
