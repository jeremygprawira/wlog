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

// TestAgentDocs_Skills proves that every skill folder holds a SKILL.md with its own
// name, and that the index names each one.
func TestAgentDocs_Skills(t *testing.T) {
	skills := filepath.Join(root(), "cmd", "wlog", "internal", "templates", "skills")
	entries, err := os.ReadDir(skills)
	if err != nil {
		t.Fatalf("read the skills folder: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(skills, "index.md"))
	if err != nil {
		t.Fatalf("read the skills index: %v", err)
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		count++
		body, err := os.ReadFile(filepath.Join(skills, entry.Name(), "SKILL.md"))
		if err != nil {
			t.Errorf("skill %s: %v", entry.Name(), err)
			continue
		}
		if !strings.Contains(string(body), "name: "+entry.Name()) {
			t.Errorf("skill %s does not name itself in its front matter", entry.Name())
		}
		if !strings.Contains(string(index), entry.Name()+"/SKILL.md") {
			t.Errorf("the index does not name the skill %s", entry.Name())
		}
	}
	if count == 0 {
		t.Error("the skills folder holds no skill")
	}
}
