// Package main tests the tidy command with a stubbed tidy runner.
//
// The stub replaces `go mod tidy -diff`, so the tests stay offline and still
// prove both outcomes: a module that tidy would change fails the check, and a
// module that tidy leaves alone passes it.
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

// TestTidy_FailsOnDiff proves that a module whose tidy would change go.mod
// reports a problem and fails the command.
func TestTidy_FailsOnDiff(t *testing.T) {
	diff := func(dir string) ([]byte, error) {
		if strings.HasSuffix(filepath.ToSlash(dir), "appmissing") {
			return []byte("--- go.mod\n+++ go.mod\n@@ -1 +1 @@\n+require example.com/lib v0.1.0\n"), nil
		}
		return nil, nil
	}

	var out bytes.Buffer
	if err := check(fixture(t), diff, &out); err == nil {
		t.Fatal("check returned nil, want an error for the untidy module")
	}
	got := out.String()
	if !strings.Contains(got, "TIDY1") || !strings.Contains(got, "appmissing") {
		t.Errorf("output misses the code or the module:\n%s", got)
	}
}

// TestTidy_PassesCleanModules proves that a module whose tidy changes nothing
// passes the check.
func TestTidy_PassesCleanModules(t *testing.T) {
	diff := func(dir string) ([]byte, error) { return nil, nil }

	var out bytes.Buffer
	if err := check(fixture(t), diff, &out); err != nil {
		t.Fatalf("check returned %v, want nil", err)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want empty", out.String())
	}
}
