package doctor_test

import (
	"bytes"
	"encoding/json"
	"io"
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
	for _, name := range []string{"middleware", "drains", "redactor"} {
		if got[name] != "fail" {
			t.Errorf("%s = %q, want fail (all: %v)", name, got[name], got)
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
		t.Errorf("exit %d, want 1 with fails", code)
	}
	lines := bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n"))
	if len(lines) != 7 {
		t.Fatalf("printed %d lines, want 7", len(lines))
	}
	for _, line := range lines {
		var check doctor.Check
		if err := json.Unmarshal(line, &check); err != nil {
			t.Errorf("line is not a JSON check: %v (%s)", err, line)
		}
		if check.Name == "" || check.Status == "" {
			t.Errorf("check is missing fields: %s", line)
		}
	}
}
