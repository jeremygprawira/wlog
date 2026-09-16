// Package main tests the snippets command over fixture documents.
//
// The fixtures hold a document whose blocks compile and run, a document with a
// block that does not compile, and a document whose sample output is wrong. Each
// test limits the run to one fixture, so a fixture never hides another.
package main

import (
	"bytes"
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

// TestSnippets_SkipsKnownBadList proves that a file on the list is skipped.
func TestSnippets_SkipsKnownBadList(t *testing.T) {
	blocks, err := collect(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 {
		t.Fatalf("collected %d blocks, want 3", len(blocks))
	}
	if blocks[0].file != "./broken.md" || blocks[1].file != "./good.md" || blocks[2].file != "./mismatch.md" {
		t.Errorf("blocks = %+v, want the three fixtures in order", blocks)
	}
}
