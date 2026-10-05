// This file tests the temporary copy: a module is copied whole, and a relative replace
// becomes an absolute one, so the copy builds outside the tree.
package tmpcopy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTmpcopy_ModuleCopiesAndAbsolutizes proves the copy holds the module files and that a
// relative replace points at the original tree.
func TestTmpcopy_ModuleCopiesAndAbsolutizes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.go"), []byte("package sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gomod := "module example.com/app\n\ngo 1.21\n\nrequire example.com/lib v0.0.0\n\nreplace example.com/lib => ../lib\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	tmp, err := Module(dir)
	if err != nil {
		t.Fatalf("Module: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if _, err := os.Stat(filepath.Join(tmp, "sub", "a.go")); err != nil {
		t.Errorf("the copy misses a file: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(tmp, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "..", "lib")
	if !strings.Contains(string(copied), "replace example.com/lib => "+want) {
		t.Errorf("the replace is not absolute:\n%s", copied)
	}
}
