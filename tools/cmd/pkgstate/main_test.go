// This file tests the pkgstate scan: it finds a package-level variable that some code
// writes, and it leaves a read-only table alone. The scan of the real tree proves that
// the only package-level state left is the default Logger pointer.
package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// writeFile writes one Go file under dir, creating the directory.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPkgState_OnlyDefaultPointer proves the scan catches the shapes of package-level
// state, and that the real tree holds only the allowed default pointer.
func TestPkgState_OnlyDefaultPointer(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "table", "table.go"), `package table

var names = []string{"a", "b"}

// Lookup reads the table, which is not state.
func Lookup(i int) string { return names[i] }
`)
	writeFile(t, filepath.Join(dir, "state", "state.go"), `package state

import "sync/atomic"

var counter atomic.Int64
var cache = map[string]int{}

// Bump writes the counter.
func Bump() { counter.Add(1) }

// Put writes the map.
func Put(key string, value int) { cache[key] = value }
`)
	found, err := scan(dir, allowed)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(found))
	for _, f := range found {
		names = append(names, f.name)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "cache" || names[1] != "counter" {
		t.Errorf("findings = %v, want the counter and the cache and no read-only table", names)
	}

	// The real tree passes, so the only package-level state is the default pointer.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	real, err := scan(root, allowed)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range real {
		t.Errorf("package-level state in the tree: %s", f)
	}
}
