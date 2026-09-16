// Package workspace_test proves the workspace reader and the affected-module walk.
//
// Each test builds a throwaway workspace in t.TempDir(). The workspace holds a
// go.work file, a root module, a library module, and an app module that requires
// the library. The tests then prove that Modules reports the path, the Go floor,
// and the dependents of each module, and that Affected adds a dependent module
// when a diff touches the module it requires.
package workspace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// writeFile writes one file and creates the parent directories.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newWorkspace builds a workspace with a root module, a library module, and an
// app module. The app requires the library, so the library has one dependent.
func newWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.work"), "go 1.21\n\nuse (\n\t.\n\t./lib\n\t./app\n)\n")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.21\n")
	writeFile(t, filepath.Join(root, "lib", "go.mod"), "module example.com/lib\n\ngo 1.22\n")
	writeFile(t, filepath.Join(root, "lib", "lib.go"), "package lib\n")
	writeFile(t, filepath.Join(root, "app", "go.mod"),
		"module example.com/app\n\ngo 1.23\n\nrequire example.com/lib v0.1.0\n")
	return root
}

// git runs one git command inside dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}
	cmd := exec.Command("git", append(base, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// moduleByDir returns the module whose dir matches, and fails when it is absent.
func moduleByDir(t *testing.T, mods []workspace.Module, dir string) workspace.Module {
	t.Helper()
	for _, m := range mods {
		if m.Dir == dir {
			return m
		}
	}
	t.Fatalf("no module with dir %q in %+v", dir, mods)
	return workspace.Module{}
}

// TestModules_ListsPathFloorAndDependents proves that Modules reads every go.work
// entry, the module path and Go floor from its go.mod, and the reverse requires.
func TestModules_ListsPathFloorAndDependents(t *testing.T) {
	mods, err := workspace.Modules(newWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 3 {
		t.Fatalf("got %d modules, want 3: %+v", len(mods), mods)
	}

	root := moduleByDir(t, mods, ".")
	if root.Path != "example.com/root" || root.Floor != "1.21" {
		t.Errorf("root = %+v, want example.com/root at 1.21", root)
	}

	lib := moduleByDir(t, mods, "./lib")
	if lib.Path != "example.com/lib" || lib.Floor != "1.22" {
		t.Errorf("lib = %+v, want example.com/lib at 1.22", lib)
	}
	if !slices.Contains(lib.Dependents, "example.com/app") {
		t.Errorf("lib dependents = %v, want example.com/app", lib.Dependents)
	}

	app := moduleByDir(t, mods, "./app")
	if app.Path != "example.com/app" || app.Floor != "1.23" {
		t.Errorf("app = %+v, want example.com/app at 1.23", app)
	}
	if !slices.Contains(app.Requires, "example.com/lib") {
		t.Errorf("app requires = %v, want example.com/lib", app.Requires)
	}
	if len(app.Dependents) != 0 {
		t.Errorf("app dependents = %v, want none", app.Dependents)
	}
}

// TestAffected_AddsDependents proves that a change inside the library module
// reports the library and the app that requires it, in a stable order.
func TestAffected_AddsDependents(t *testing.T) {
	root := newWorkspace(t)
	git(t, root, "init", "-q")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "root commit")

	writeFile(t, filepath.Join(root, "lib", "lib.go"), "package lib\n\n// changed\n")

	got, err := workspace.Affected(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./app", "./lib"}
	if !slices.Equal(got, want) {
		t.Errorf("Affected = %v, want %v", got, want)
	}
}

// TestAffected_IncludesRootModule proves that a change to a root module file
// reports the root module, whose dir is ".".
func TestAffected_IncludesRootModule(t *testing.T) {
	root := newWorkspace(t)
	git(t, root, "init", "-q")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "root commit")

	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.22\n")

	got, err := workspace.Affected(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"."}
	if !slices.Equal(got, want) {
		t.Errorf("Affected = %v, want %v", got, want)
	}
}

// TestAffected_IgnoresUnrelatedFiles proves that a file outside every module
// reports no module.
func TestAffected_IgnoresUnrelatedFiles(t *testing.T) {
	root := newWorkspace(t)
	git(t, root, "init", "-q")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "root commit")

	writeFile(t, filepath.Join(root, "Makefile"), "all:\n")

	got, err := workspace.Affected(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Affected = %v, want no module", got)
	}
}
