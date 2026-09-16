// Package main tests the floor command over a fixture workspace.
//
// Most tests replace the go command with a stub, so they run in milliseconds and
// prove the selection and the reporting. One test runs the real go command on a
// module that claims a Go 1.21 floor while it uses a Go 1.22 range form, which
// proves the check fails when the floor is a lie.
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
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "floor"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestFloor_RunsEveryModuleAtItsOwnFloor proves that the command hands the go
// command the floor from each go.mod, and that one failure fails the run.
func TestFloor_RunsEveryModuleAtItsOwnFloor(t *testing.T) {
	floors := map[string]string{}
	run := func(dir, floor string) ([]byte, error) {
		floors[filepath.Base(dir)] = floor
		if filepath.Base(dir) == "new" {
			return []byte("./new.go:12:5: cannot range over 3 (untyped int constant)\n"), &stubError{}
		}
		return nil, nil
	}

	var out bytes.Buffer
	if err := check(fixture(t), nil, run, &out); err == nil {
		t.Fatal("check returned nil, want an error for the module that lies about its floor")
	}
	if floors["new"] != "1.21" || floors["old"] != "1.21" {
		t.Errorf("floors = %v, want 1.21 for new and old", floors)
	}
	got := out.String()
	if !strings.Contains(got, "FLOOR1") || !strings.Contains(got, "cannot range over 3") {
		t.Errorf("output misses the code or the go error:\n%s", got)
	}
}

// TestFloor_LimitsToGivenDirs proves that directory arguments limit the run.
func TestFloor_LimitsToGivenDirs(t *testing.T) {
	var ran []string
	run := func(dir, floor string) ([]byte, error) {
		ran = append(ran, filepath.Base(dir))
		return nil, nil
	}

	var out bytes.Buffer
	if err := check(fixture(t), []string{"./old"}, run, &out); err != nil {
		t.Fatalf("check returned %v, want nil", err)
	}
	if len(ran) != 1 || ran[0] != "old" {
		t.Errorf("ran %v, want only old", ran)
	}
}

// TestFloor_UnknownDirFails proves that a directory that names no module is an
// error, so a typo never turns into a silent pass.
func TestFloor_UnknownDirFails(t *testing.T) {
	run := func(dir, floor string) ([]byte, error) { return nil, nil }

	var out bytes.Buffer
	if err := check(fixture(t), []string{"./nope"}, run, &out); err == nil {
		t.Fatal("check returned nil, want an error for the unknown dir")
	}
}

// TestFloor_ToolchainMapsLanguageVersion proves that a language floor becomes a
// full toolchain version, so the go command accepts it.
func TestFloor_ToolchainMapsLanguageVersion(t *testing.T) {
	for _, tc := range []struct{ floor, want string }{
		{"1.21", "go1.21.0"},
		{"1.26", "go1.26.0"},
		{"1.25.0", "go1.25.0"},
		{"1.26.1", "go1.26.1"},
	} {
		if got := toolchain(tc.floor); got != tc.want {
			t.Errorf("toolchain(%q) = %q, want %q", tc.floor, got, tc.want)
		}
	}
}

// stubError stands in for a failed go command.
type stubError struct{}

// Error returns the text of a build failure.
func (stubError) Error() string { return "exit status 1" }

// TestFloor_FailsOnNewerFeature runs the real go command on a module that claims
// a Go 1.21 floor while it uses a Go 1.22 range form. The check must fail.
func TestFloor_FailsOnNewerFeature(t *testing.T) {
	var out bytes.Buffer
	err := check(fixture(t), []string{"./new"}, runFloor, &out)
	if err == nil {
		t.Fatalf("check returned nil, want a failure at the floor:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "FLOOR1") {
		t.Errorf("output misses the FLOOR1 code:\n%s", out.String())
	}
}

// TestFloor_PassesModuleAtItsFloor runs the real go command on a module whose
// code matches its floor. The check must pass.
func TestFloor_PassesModuleAtItsFloor(t *testing.T) {
	var out bytes.Buffer
	if err := check(fixture(t), []string{"./old"}, runFloor, &out); err != nil {
		t.Fatalf("check returned %v, want nil:\n%s", err, out.String())
	}
}
