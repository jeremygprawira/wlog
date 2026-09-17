package doctor_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
)

// statuses returns the status of each named check.
func statuses(checks []doctor.Check) map[string]string {
	out := map[string]string{}
	for _, check := range checks {
		out[check.Name] = check.Status
	}
	return out
}

// TestDoctor_FixtureFails proves the three named problems each produce a fail.
func TestDoctor_FixtureFails(t *testing.T) {
	t.Setenv("AXIOM_TOKEN", "")
	t.Setenv("AXIOM_DATASET", "")

	got := statuses(doctor.Inspect("../../testdata/doctorbad"))
	// The fixture cannot load, so the load check fails and the two checks that read files report
	// what they find. The type-based checks say they did not run instead of claiming a pass.
	for _, name := range []string{"load", "drains", "redactor"} {
		if got[name] != "fail" {
			t.Errorf("%s = %q, want fail (all: %v)", name, got[name], got)
		}
	}
	for _, name := range []string{"middleware", "logger", "score"} {
		if got[name] != "warn" {
			t.Errorf("%s = %q, want warn: the package did not load (all: %v)", name, got[name], got)
		}
	}
}

// TestDoctor_ExamplesPasses proves the repository's own examples have no fail.
func TestDoctor_ExamplesPasses(t *testing.T) {
	checks := doctor.Inspect("../../../../examples")
	for _, check := range checks {
		if check.Status == "fail" {
			t.Errorf("%s failed on examples: %s", check.Name, check.Message)
		}
	}
}

// TestDoctor_JSON proves --json prints one object per check.
func TestDoctor_JSON(t *testing.T) {
	var stdout bytes.Buffer
	if code := doctor.Run([]string{"--dir", "../../testdata/doctorbad", "--json"}, &stdout, io.Discard); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	var document struct {
		Version int            `json:"version"`
		Passed  bool           `json:"passed"`
		Checks  []doctor.Check `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("--json printed no single object: %v\n%s", err, stdout.String())
	}
	names := map[string]bool{}
	for _, check := range document.Checks {
		names[check.Name] = true
	}
	for _, want := range []string{"load", "module", "adapters", "middleware", "logger", "drains", "redactor", "score"} {
		if !names[want] {
			t.Errorf("the document holds no %s check: %v", want, names)
		}
	}
	if document.Passed {
		t.Error("passed = true for a fixture with failures")
	}
}

// TestDoctor_CLI10_LoadError proves a directory whose package cannot load is reported as a
// failure, instead of every line printing PASS for an app that was never read.
func TestDoctor_CLI10_LoadError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := doctor.Run([]string{"--dir", "../../testdata/doctorbroken"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit %d, want 1 for a load error\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "WLOG_DOCTOR_LOAD") {
		t.Errorf("the output names no load code:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String()+stderr.String(), "doctorbroken") {
		t.Errorf("the output does not name the directory it read:\n%s%s", stdout.String(), stderr.String())
	}

	// The check list itself carries the failure, so a caller that reads it sees the same.
	found := false
	for _, check := range doctor.Inspect("../../testdata/doctorbroken") {
		if check.Code == "WLOG_DOCTOR_LOAD" && check.Status == "fail" {
			found = true
		}
	}
	if !found {
		t.Error("Inspect reported no failing load check")
	}
}

// TestDoctor_PAR34_CodesJSON proves every finding carries a WLOG_DOCTOR_* code with why and fix,
// and that --json prints exactly one object.
func TestDoctor_PAR34_CodesJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := doctor.Run([]string{"--dir", "../../testdata/doctorbad", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s", code, stderr.String())
	}

	var document struct {
		Version int  `json:"version"`
		Passed  bool `json:"passed"`
		Checks  []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Code   string `json:"code"`
			Why    string `json:"why"`
			Fix    string `json:"fix"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("--json did not print one object: %v\n%s", err, stdout.String())
	}
	if len(document.Checks) == 0 {
		t.Fatal("the document holds no checks")
	}
	for _, check := range document.Checks {
		if !strings.HasPrefix(check.Code, "WLOG_DOCTOR_") {
			t.Errorf("check %q has code %q, want a WLOG_DOCTOR_* code", check.Name, check.Code)
		}
		if strings.TrimSpace(check.Why) == "" || strings.TrimSpace(check.Fix) == "" {
			t.Errorf("check %q carries no why or no fix: %+v", check.Name, check)
		}
	}
}
