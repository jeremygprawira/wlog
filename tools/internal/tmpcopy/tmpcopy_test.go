// This file tests the temporary copy: the whole workspace is copied, the history is left
// behind, and a relative path inside the copy still resolves.
package tmpcopy

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTmpcopy_WorkspaceCopiesTheTree proves the copy holds the files, keeps the history
// that a baseline test reads, and keeps the relative layout that a module's tests depend
// on.
func TestTmpcopy_WorkspaceCopiesTheTree(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"cmd/app/main.go", "examples/recipe/main.go", ".git/config"} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tmp, err := Workspace(root)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	// A module in the copy reads a sibling by a relative path that climbs four levels.
	recipe := filepath.Join(tmp, "cmd", "app", "..", "..", "examples", "recipe", "main.go")
	if _, err := os.Stat(filepath.Clean(recipe)); err != nil {
		t.Errorf("the copy cannot resolve a sibling path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".git", "config")); err != nil {
		t.Errorf("the copy misses the history that a baseline test reads: %v", err)
	}
}
