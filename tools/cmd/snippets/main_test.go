// Package main tests the snippets command over fixture documents.
//
// The fixtures hold a document whose blocks compile and run, a document with a
// block that does not compile, and a document whose sample output is wrong. Each
// test limits the run to one fixture, so a fixture never hides another.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture returns the path of the fixture directory.
func fixture(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "snippets"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestSnippets_CompilesAndRunsTheGoodDocument proves that a block which compiles,
// and a block whose output matches its text block, pass.
func TestSnippets_CompilesAndRunsTheGoodDocument(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), "good.md", &out); err != nil {
		t.Fatalf("run returned %v, want nil:\n%s", err, out.String())
	}
}

// TestSnippets_FailsOnBrokenBlock proves that a block which does not compile
// reports SNIP1 and the file line.
func TestSnippets_FailsOnBrokenBlock(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), "broken.md", &out); err == nil {
		t.Fatal("run returned nil, want an error for the broken block")
	}
	got := out.String()
	if !strings.Contains(got, "SNIP1") || !strings.Contains(got, "broken.md") {
		t.Errorf("output misses the code or the file:\n%s", got)
	}
}

// TestSnippets_FailsOnWrongOutput proves that a run block whose output differs
// from the text block after it reports SNIP2.
func TestSnippets_FailsOnWrongOutput(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), "mismatch.md", &out); err == nil {
		t.Fatal("run returned nil, want an error for the wrong output")
	}
	got := out.String()
	if !strings.Contains(got, "SNIP2") || !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Errorf("output misses the code, the got text, or the want text:\n%s", got)
	}
}

// TestSnippets_CollectsEveryBlock proves the command has no file-level skip list: every Go
// block of a document is collected, and a sketch is skipped by its own marker.
func TestSnippets_CollectsEveryBlock(t *testing.T) {
	blocks, err := collect(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 4 {
		t.Fatalf("collected %d blocks, want 4", len(blocks))
	}
	files := []string{}
	for _, b := range blocks {
		files = append(files, b.file)
	}
	for _, want := range []string{"./broken.md", "./good.md", "./mismatch.md", "./sketch.md"} {
		if !strings.Contains(strings.Join(files, " "), want) {
			t.Errorf("the walk missed %s: %v", want, files)
		}
	}
	for _, b := range blocks {
		if b.file == "./sketch.md" && !b.sketch {
			t.Error("the sketch block is not marked as a sketch")
		}
	}
}

// TestSnippets_SkipsASketchBlock proves a block marked as a sketch is not built, while the
// blocks around it still are. A spec shows a shape, and a guide shows a program.
func TestSnippets_SkipsASketchBlock(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), "sketch.md", &out); err != nil {
		t.Fatalf("run returned %v, want nil for a marked sketch:\n%s", err, out.String())
	}

	// The marker is per block: the same shape without it is a program, and it fails.
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join(fixture(t), "sketch.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	unmarked := strings.Replace(string(source), "<!-- snippet:sketch -->\n", "", 1)
	if err := os.WriteFile(filepath.Join(dir, "unmarked.md"), []byte(unmarked), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := run(dir, "unmarked.md", &out); err == nil {
		t.Error("an unmarked shape passed, want a build failure")
	}
}
