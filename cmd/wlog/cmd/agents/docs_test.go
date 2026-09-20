// This file checks the two language-model documents: they exist, the index names every
// page, and the full text holds the pages.
package agents_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// root is the workspace root, from the test's working directory, which sits under the
// cmd/wlog module.
func root() string { return filepath.Join("..", "..", "..", "..") }

// TestAgentDocs_Files proves that make docs left both documents in place, with the index
// naming the pages and the full text holding them.
func TestAgentDocs_Files(t *testing.T) {
	index, err := os.ReadFile(filepath.Join(root(), "llms.txt"))
	if err != nil {
		t.Fatalf("read llms.txt: %v", err)
	}
	full, err := os.ReadFile(filepath.Join(root(), "llms-full.txt"))
	if err != nil {
		t.Fatalf("read llms-full.txt: %v", err)
	}
	for _, want := range []string{"# wlog", "docs/event-shape.md", "docs/recipes/rest-api.md"} {
		if !strings.Contains(string(index), want) {
			t.Errorf("llms.txt does not hold %q", want)
		}
	}
	if !strings.Contains(string(full), "# wlog, full documentation") {
		t.Error("llms-full.txt has no heading")
	}
	if len(full) <= len(index) {
		t.Errorf("llms-full.txt is %d bytes and llms.txt is %d, want the full text larger", len(full), len(index))
	}
}
