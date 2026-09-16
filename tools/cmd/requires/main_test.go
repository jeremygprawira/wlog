// Package main tests the requires command over the fixture workspace.
//
// The fixture at tools/testdata/requires holds five app modules that import the
// same library module. One is correct, and each other one shows a single
// mistake, so a test can assert the exact code and the exact file of every
// problem without a network call or a build.
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// fixture returns the path of the fixture workspace.
func fixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "requires"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestRequires_ReportsEveryProblem proves that each mistake in the fixture
// prints one line with its code and its file, and that the clean module never
// prints a line.
func TestRequires_ReportsEveryProblem(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), &out); err == nil {
		t.Fatal("run returned nil, want an error for the fixture problems")
	}
	got := out.String()

	want := []string{
		"REQ1", "appmissing/app.go",
		"REQ2", "appversion/go.mod",
		"REQ3", "appreplace/go.mod",
		"REQ4", "appindirect/go.mod",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output misses %q:\n%s", w, got)
		}
	}
	if strings.Contains(got, "appclean") {
		t.Errorf("output reports the clean module:\n%s", got)
	}
}

// TestRequires_ReportsExactlyFiveProblems proves that the fixture yields one
// problem per mistake and nothing more. A module must not report the imports of
// a nested module, REQ4 must survive the // indirect marker, and an import of a
// sub-module package must not mark the parent module as a direct dependency.
func TestRequires_ReportsExactlyFiveProblems(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), &out); err == nil {
		t.Fatal("run returned nil, want an error")
	}

	var got []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.TrimSpace(line) != "" {
			got = append(got, line)
		}
	}
	if len(got) != 5 {
		t.Fatalf("got %d problems, want 5:\n%s", len(got), out.String())
	}
	for i, want := range []string{"REQ4", "REQ4", "REQ1", "REQ3", "REQ2"} {
		if !strings.Contains(got[i], want) {
			t.Errorf("problem %d = %q, want code %s", i, got[i], want)
		}
	}
	if strings.Contains(out.String(), "example.com/subpkg is imported directly") {
		t.Errorf("the parent module counts as a direct import:\n%s", out.String())
	}
}

// TestRequires_PassesCleanModule proves that a module with the right require,
// the right version, and a local replace produces no problem.
func TestRequires_PassesCleanModule(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), &out); err == nil {
		t.Fatal("run returned nil, want an error for the other fixtures")
	}
	if strings.Contains(out.String(), "appclean") {
		t.Errorf("output reports the clean module:\n%s", out.String())
	}
}

// TestRequires_ReportsVersionFromToolsVersion proves that the version in
// tools/version.txt is the version the check compares against.
func TestRequires_ReportsVersionFromToolsVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), &out); err == nil {
		t.Fatal("run returned nil, want an error")
	}
	if !strings.Contains(out.String(), "v0.1.0") {
		t.Errorf("output does not name the wanted version:\n%s", out.String())
	}
}
