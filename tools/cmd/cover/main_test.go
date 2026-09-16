// Package main tests the cover command with a stubbed test run.
//
// The stub replaces `go test -cover`, so the tests stay offline and prove the
// parse, the ratchet, and the report.
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// fixture returns the fixture directory, which holds a known-low list.
func fixture(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "cover"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// output is one go test cover output with three packages: one over the minimum,
// one under it and on the known-low list, and one under it and unlisted.
const output = `ok  	example.com/low	0.3s	coverage: 71.4% of statements
ok  	example.com/listed	0.2s	coverage: 60.0% of statements
ok  	example.com/good	0.1s	coverage: 92.5% of statements
?   	example.com/none	[no test files]
`

// TestCover_ReportsOnlyUnlistedPackages proves that a package under the minimum
// fails the run, and that a package on the known-low list does not.
func TestCover_ReportsOnlyUnlistedPackages(t *testing.T) {
	run := func(dir string) ([]byte, error) { return []byte(output), nil }

	var out bytes.Buffer
	if err := check(fixture(t), 85, run, &out); err == nil {
		t.Fatal("check returned nil, want an error for the unlisted package")
	}
	got := out.String()
	if !strings.Contains(got, "example.com/low") || !strings.Contains(got, "COV1") {
		t.Errorf("output misses the package or the code:\n%s", got)
	}
	if strings.Contains(got, "example.com/listed") || strings.Contains(got, "example.com/good") {
		t.Errorf("output reports a listed or a passing package:\n%s", got)
	}
}

// TestCover_PassesWhenEveryLowPackageIsListed proves that a run whose low
// packages are all on the list reports nothing and passes.
func TestCover_PassesWhenEveryLowPackageIsListed(t *testing.T) {
	text := `ok  	example.com/listed	0.2s	coverage: 60.0% of statements
ok  	example.com/good	0.1s	coverage: 92.5% of statements
`
	run := func(dir string) ([]byte, error) { return []byte(text), nil }

	var out bytes.Buffer
	if err := check(fixture(t), 85, run, &out); err != nil {
		t.Fatalf("check returned %v, want nil:\n%s", err, out.String())
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want empty", out.String())
	}
}

// TestCover_ReadsEveryPackage proves that the parse keeps the package and its
// percentage, and skips a package with no test files.
func TestCover_ReadsEveryPackage(t *testing.T) {
	results := parseCoverage(output)
	if len(results) != 3 {
		t.Fatalf("parsed %d packages, want 3: %+v", len(results), results)
	}
	want := []result{
		{pkg: "example.com/good", percent: 92.5},
		{pkg: "example.com/listed", percent: 60.0},
		{pkg: "example.com/low", percent: 71.4},
	}
	for i, w := range want {
		if results[i] != w {
			t.Errorf("package %d = %+v, want %+v", i, results[i], w)
		}
	}
}
