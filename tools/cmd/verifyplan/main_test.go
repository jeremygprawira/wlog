// Package main tests the verifyplan command over a fixture plan.
//
// The fixture holds three tasks: one whose Verify command runs a test, one whose
// -run pattern matches nothing, and one that describes a push instead of a
// command. The tests prove that the command runs the first, fails the second,
// and skips the third.
package main

import (
	"bytes"
	"os"
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
	if len(tasks) != 3 {
		t.Fatalf("parsed %d tasks, want 3", len(tasks))
	}
	for _, task := range tasks {
		if task.id == "1-PROSE-1" {
			t.Errorf("parse kept the prose task: %+v", task)
		}
	}
	wantIDs := []string{"1-OK-1", "1-BAD-1", "1-MULTI-1"}
	for i, want := range wantIDs {
		if tasks[i].id != want {
			t.Errorf("tasks[%d].id = %q, want %q", i, tasks[i].id, want)
		}
	}
	if tasks[0].line >= tasks[1].line || tasks[1].line >= tasks[2].line {
		t.Errorf("tasks = %+v, want ascending line numbers", tasks)
	}
}

// TestVerifyPlan_MultiPackagePassIsNotAFalsePositive proves that a Verify command
// running ./... over several packages passes when its pattern matches a real test
// in one of them, even though every sibling package prints "no tests to run".
func TestVerifyPlan_MultiPackagePassIsNotAFalsePositive(t *testing.T) {
	var out bytes.Buffer
	if err := run(toolsRoot(t), fixture(t), "1-MULTI-1", &out); err != nil {
		t.Fatalf("run returned %v, want nil:\n%s", err, out.String())
	}
}

// TestVerifyplan_AddsVerboseToEveryGoTest proves that a compound command gets the flag
// on every go test, so the run that matches the pattern is visible even when an earlier
// package holds no test.
func TestVerifyplan_AddsVerboseToEveryGoTest(t *testing.T) {
	command := "go test -race ./schema && cd tools && go test -race -run 'TestSchema_' ./... && go run ./cmd/schema"
	want := "go test -v -race ./schema && cd tools && go test -v -race -run 'TestSchema_' ./... && go run ./cmd/schema"
	if got := addVerbose(command); got != want {
		t.Errorf("addVerbose = %q, want %q", got, want)
	}
}

// TestVerifyPlan_MapsAuditIDs proves that an audit id maps to the first plan task that
// names it, and that the prefix of a plan task id does not become an id of its own.
func TestVerifyPlan_MapsAuditIDs(t *testing.T) {
	dir := t.TempDir()
	audit := filepath.Join(dir, "audit.md")
	plan := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(audit, []byte("| PAR-1 | a | b |\n| BET-7 | c | d |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan, []byte("#### 10-AUD-2 The fix\n\nCloses PAR-1 and BET-7.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ids, err := auditIDs(audit)
	if err != nil {
		t.Fatalf("auditIDs: %v", err)
	}
	if strings.Join(ids, ",") != "BET-7,PAR-1" {
		t.Errorf("ids = %v, want BET-7 then PAR-1", ids)
	}

	named, err := planNames(plan)
	if err != nil {
		t.Fatalf("planNames: %v", err)
	}
	for _, id := range []string{"PAR-1", "BET-7"} {
		if len(named[id]) != 1 || named[id][0] != "10-AUD-2" {
			t.Errorf("named[%s] = %v, want 10-AUD-2", id, named[id])
		}
	}
	if _, ok := named["AUD-2"]; ok {
		t.Errorf("the task prefix became an id: %v", named)
	}
}

// TestVerifyPlan_ReportsAnIdWithNoTask proves that an audit id which no plan task names
// fails the audit run.
func TestVerifyPlan_ReportsAnIdWithNoTask(t *testing.T) {
	dir := t.TempDir()
	audit := filepath.Join(dir, "audit.md")
	plan := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(audit, []byte("| PAR-1 | a | b |\n| PAR-2 | c | d |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan, []byte("#### 10-A-1 The fix\n\nCloses PAR-1.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := auditRun(dir, audit, plan, &out); err == nil {
		t.Fatal("auditRun returned nil, want an error for PAR-2")
	}
	if !strings.Contains(out.String(), "AUD1") || !strings.Contains(out.String(), "PAR-2") {
		t.Errorf("the report misses the id:\n%s", out.String())
	}
}
