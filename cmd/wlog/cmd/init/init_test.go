package init_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	wloginit "github.com/jeremygprawira/wlog/cmd/wlog/cmd/init"
)

// caseDef describes one framework fixture and the modules its generated tree needs.
type caseDef struct {
	framework string
	requires  []string
	replaces  map[string]string // module path -> path under the repo
}

// cases lists every framework init must support.
func cases() []caseDef {
	return []caseDef{
		{framework: "nethttp"},
		{framework: "mux", requires: []string{"github.com/gorilla/mux v1.8.1"}},
		{framework: "echo", requires: []string{"github.com/labstack/echo/v4 v4.15.4"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/middleware/echo": "middleware/echo"}},
		{framework: "echo5", requires: []string{"github.com/labstack/echo/v5 v5.3.1"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/middleware/echo5": "middleware/echo5"}},
		{framework: "gin", requires: []string{"github.com/gin-gonic/gin v1.12.0"},
			replaces: map[string]string{"github.com/jeremygprawira/wlog/middleware/gin": "middleware/gin"}},
	}
}

// repoRoot returns the repository root, three levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return root
}

// moduleFloor reads the go line of a go.mod file.
func moduleFloor(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	t.Fatalf("%s has no go line", path)
	return ""
}

// treeFor copies a fixture into a temp directory and writes its go.mod.
func treeFor(t *testing.T, tc caseDef, root string) string {
	t.Helper()
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "inittrees", tc.framework, "main.go"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// The generated tree needs one floor: the floor of this module, because it
	// depends on the modules this module depends on.
	goMod := "module example.com/app\n\ngo " + moduleFloor(t, filepath.Join("..", "..", "go.mod")) +
		"\n\nrequire (\n\tgithub.com/jeremygprawira/wlog v0.0.0\n"
	for _, require := range tc.requires {
		goMod += "\t" + require + "\n"
	}
	for module := range tc.replaces {
		goMod += "\t" + module + " v0.0.0\n"
	}
	goMod += ")\n\nreplace github.com/jeremygprawira/wlog => " + root + "\n"
	for module, path := range tc.replaces {
		goMod += "replace " + module + " => " + filepath.Join(root, filepath.FromSlash(path)) + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	return dir
}

// TestInit_BuildsEveryFramework proves the generated tree compiles for all five.
func TestInit_BuildsEveryFramework(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range cases() {
		t.Run(tc.framework, func(t *testing.T) {
			dir := treeFor(t, tc, root)
			if code := wloginit.Run([]string{"--dir", dir, "--framework", tc.framework}, io.Discard, io.Discard); code != 0 {
				t.Fatalf("init exit %d", code)
			}
			for _, name := range []string{"wlog.go", ".env.example"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatalf("%s missing: %v", name, err)
				}
			}
			build := exec.Command("go", "build", "./...")
			build.Dir = dir
			build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("generated tree does not build: %v\n%s", err, output)
			}
		})
	}
}

// TestInit_DryRunWritesNothing proves dry-run leaves the tree alone.
func TestInit_DryRunWritesNothing(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp"}, root)

	var stdout bytes.Buffer
	if code := wloginit.Run([]string{"--dir", dir, "--dry-run"}, &stdout, io.Discard); code != 0 {
		t.Fatalf("dry run exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "wlog.go")); !os.IsNotExist(err) {
		t.Error("dry run wrote wlog.go")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("wlog.go")) {
		t.Errorf("dry run printed no plan:\n%s", stdout.String())
	}
}

// TestInit_ExistingSetupFails proves an existing wlog.go is never overwritten.
func TestInit_ExistingSetupFails(t *testing.T) {
	root := repoRoot(t)
	dir := treeFor(t, caseDef{framework: "nethttp"}, root)
	if err := os.WriteFile(filepath.Join(dir, "wlog.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("seed wlog.go: %v", err)
	}
	if code := wloginit.Run([]string{"--dir", dir}, io.Discard, io.Discard); code != 1 {
		t.Errorf("exit %d, want 1 when wlog.go already exists", code)
	}
}
