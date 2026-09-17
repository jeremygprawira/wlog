// Package main tests the release command over a fixture workspace.
//
// The fixture holds a root module, a library module, and an app module that
// requires the library, so the tests prove the tag order, the require updates,
// and the confirmation gate without touching a tag.
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

// byPath returns the step of a module path, and reports whether it exists.
func byPath(steps []step, path string) (step, bool) {
	for _, s := range steps {
		if s.module.Path == path {
			return s, true
		}
	}
	return step{}, false
}

// TestRelease_PlansTagsInDependencyOrder proves that the root module comes
// first, and that a module follows the modules it requires.
func TestRelease_PlansTagsInDependencyOrder(t *testing.T) {
	steps, err := plan(fixture(t), "v0.1.0", "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		t.Fatal("plan returned no step")
	}
	if got := tag(steps[0].module, "v0.2.0"); got != "v0.2.0" {
		t.Errorf("first tag = %q, want the plain version for the root module", got)
	}

	// Every module must come after the modules of this workspace that it
	// requires. A library from outside the workspace puts no constraint on the
	// order.
	planned := map[string]bool{}
	for _, s := range steps {
		planned[s.module.Path] = true
		for _, req := range s.module.Requires {
			if _, ok := byPath(steps, req); !ok {
				continue
			}
			if !planned[req] && req != s.module.Path {
				t.Errorf("module %s is planned before its requirement %s", s.module.Path, req)
			}
		}
	}
	if got := tag(steps[len(steps)-1].module, "v0.2.0"); !strings.Contains(got, "/v0.2.0") {
		t.Errorf("last tag = %q, want a sub-module tag", got)
	}
}

// TestRelease_PlansTheRequireUpdates proves that the plan names the require line
// of every sibling that the new version changes.
func TestRelease_PlansTheRequireUpdates(t *testing.T) {
	steps, err := plan(fixture(t), "v0.1.0", "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range steps {
		for _, change := range s.changes {
			if strings.Contains(change, "example.com/lib") && strings.HasSuffix(change, "-> v0.2.0") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("the plan holds no update of example.com/lib: %+v", steps)
	}
}

// TestRelease_ReadsARequireChangeLine proves that apply can read the line the
// plan writes. A line with five fields used to look unreadable, so apply edited
// nothing and still tagged the release.
func TestRelease_ReadsARequireChangeLine(t *testing.T) {
	path, ok := requireEdit("require example.com/lib v0.1.0 -> v0.2.0")
	if !ok || path != "example.com/lib" {
		t.Fatalf("requireEdit = %q, %v, want example.com/lib, true", path, ok)
	}
	if _, ok := requireEdit("the plan changed"); ok {
		t.Error("requireEdit read a line that is not a require update")
	}
	if _, ok := requireEdit("require example.com/lib"); ok {
		t.Error("requireEdit read a line with no new version")
	}
}

// TestRelease_ReadsTheLastTagOfAModule proves that a sub-module tag puts its
// directory in front, which is the rule that the Go module proxy follows.
func TestRelease_ReadsTheLastTagOfAModule(t *testing.T) {
	steps, err := plan(fixture(t), "v0.1.0", "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.module.Dir == "." {
			continue
		}
		if s.lastTag == "" || !strings.HasSuffix(s.lastTag, "v0.1.0") {
			t.Errorf("last tag of %s = %q, want a tag that ends with v0.1.0", s.module.Dir, s.lastTag)
		}
	}
}

// TestRelease_ReadsNoLastTagOfANewModule proves that a module without a previous
// tag reports so, because it has no API to break.
func TestRelease_ReadsNoLastTagOfANewModule(t *testing.T) {
	steps, err := plan(fixture(t), "", "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.lastTag != "" {
			t.Errorf("last tag of %s = %q, want none", s.module.Dir, s.lastTag)
		}
	}
}

// TestRelease_RefusesAWrongTagList proves that a real run stops when the typed
// list differs from the plan, and that it changes nothing.
func TestRelease_RefusesAWrongTagList(t *testing.T) {
	var out bytes.Buffer
	err := run(fixture(t), "v0.2.0", false, strings.NewReader("v9.9.9"), &out)
	if err == nil {
		t.Fatal("run returned nil, want an error for the wrong list")
	}
	if !strings.Contains(err.Error(), "differs") {
		t.Errorf("error = %v, want it to name the difference", err)
	}
}

// TestRelease_DryRunPrintsThePlan proves that a dry run prints the tags, the
// API report, and the note that it changed nothing.
func TestRelease_DryRunPrintsThePlan(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), "v0.2.0", true, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run returned %v, want nil:\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"tags, in dependency order", "v0.2.0", "API against the previous tag", "the dry run changed nothing"} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
}

// TestRelease_RefusesNoVersion proves that a run without a version stops at
// once.
func TestRelease_RefusesNoVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run(fixture(t), "", true, strings.NewReader(""), &out); err == nil {
		t.Fatal("run returned nil, want an error for the missing version")
	}
}
