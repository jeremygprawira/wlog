// Package main tests the verifyplan command over a fixture plan.
//
// The fixture holds three tasks: one whose Verify command runs a test, one whose
// -run pattern matches nothing, and one that describes a push instead of a
// command. The tests prove that the command runs the first, fails the second,
// and skips the third.
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// fixture returns the path of the fixture plan.
func fixture(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", "verifyplan", "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// toolsRoot returns the tools module root, which is the working directory that
// the fixture commands use.
func toolsRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestVerifyPlan_FailsOnEmptyRun proves that a -run pattern which matches no test
// fails, even though the go command exits 0.
func TestVerifyPlan_FailsOnEmptyRun(t *testing.T) {
	var out bytes.Buffer
	if err := run(toolsRoot(t), fixture(t), "1-BAD-1", &out); err == nil {
		t.Fatal("run returned nil, want an error for the empty -run pattern")
	}
	got := out.String()
	if !strings.Contains(got, "VP1") || !strings.Contains(got, "1-BAD-1") {
		t.Errorf("output misses the code or the task id:\n%s", got)
	}
	if !strings.Contains(got, "matches no test") {
		t.Errorf("output does not explain the problem:\n%s", got)
	}
}

// TestVerifyPlan_PassesWhenTheTestRuns proves that a Verify command whose pattern
// matches a test passes.
func TestVerifyPlan_PassesWhenTheTestRuns(t *testing.T) {
	var out bytes.Buffer
	if err := run(toolsRoot(t), fixture(t), "1-OK-1", &out); err != nil {
		t.Fatalf("run returned %v, want nil:\n%s", err, out.String())
	}
}

// TestVerifyPlan_SkipsProse proves that a Verify line which holds no command is
// skipped, so the plan may describe a check that a tool cannot run.
func TestVerifyPlan_SkipsProse(t *testing.T) {
	tasks, err := parse(fixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("parsed %d tasks, want 2", len(tasks))
	}
	for _, task := range tasks {
		if task.id == "1-PROSE-1" {
			t.Errorf("parse kept the prose task: %+v", task)
		}
	}
	if tasks[0].id != "1-OK-1" || tasks[1].id != "1-BAD-1" || tasks[0].line >= tasks[1].line {
		t.Errorf("tasks = %+v, want 1-OK-1 then 1-BAD-1", tasks)
	}
}
