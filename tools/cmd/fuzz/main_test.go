// Package main tests the fuzz command over a fixture workspace.
//
// The fixture holds one module with two fuzz targets and one plain test, so the
// test proves that the finder reports the targets and skips the test.
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
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fuzz"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestFuzz_FindsEveryTarget proves that the finder reports both targets of the
// fixture, with the package that holds them, and skips the plain test.
func TestFuzz_FindsEveryTarget(t *testing.T) {
	targets, err := find(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("found %d targets, want 2: %+v", len(targets), targets)
	}
	if targets[0].name != "FuzzAdd" || targets[1].name != "FuzzSub" {
		t.Errorf("targets = %+v, want FuzzAdd then FuzzSub", targets)
	}
	if targets[0].pkg != "." || targets[0].module != "./app" {
		t.Errorf("target = %+v, want the app package of the app module", targets[0])
	}
}

// TestFuzz_ReportsAFailedTarget proves that a target which fails stops the run
// and reports the code, the target, and the output.
func TestFuzz_ReportsAFailedTarget(t *testing.T) {
	var out bytes.Buffer
	runTarget := func(dir, pkg, name, duration string) ([]byte, error) {
		if name == "FuzzSub" {
			return []byte("--- FAIL: FuzzSub\n    crash found\n"), &stubError{}
		}
		return nil, nil
	}
	if err := run(fixture(t), "1s", "", runTarget, &out); err == nil {
		t.Fatal("run returned nil, want an error for the failed target")
	}
	got := out.String()
	if !strings.Contains(got, "FUZZ1") || !strings.Contains(got, "FuzzSub") || !strings.Contains(got, "crash found") {
		t.Errorf("output misses the code, the target, or the output:\n%s", got)
	}
	if !strings.Contains(got, "fuzz FuzzAdd") {
		t.Errorf("the run skipped the first target:\n%s", got)
	}
}

// TestFuzz_OnlyLimitsTheRun proves that -only keeps the matching targets.
func TestFuzz_OnlyLimitsTheRun(t *testing.T) {
	var ran []string
	runTarget := func(dir, pkg, name, duration string) ([]byte, error) {
		ran = append(ran, name)
		return nil, nil
	}
	var out bytes.Buffer
	if err := run(fixture(t), "1s", "Sub", runTarget, &out); err != nil {
		t.Fatalf("run returned %v, want nil", err)
	}
	if len(ran) != 1 || ran[0] != "FuzzSub" {
		t.Errorf("ran %v, want only FuzzSub", ran)
	}
}

// stubError stands in for a failed fuzz run.
type stubError struct{}

// Error returns the text of a failed run.
func (stubError) Error() string { return "exit status 1" }
