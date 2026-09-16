package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// runMap runs the map command and returns its exit code, stdout, and stderr.
func runMap(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"map"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// TestGolden proves the map is byte-identical across runs and matches the pinned file.
// Run with UPDATE_GOLDEN=1 to regenerate a golden file after a deliberate change.
func TestGolden(t *testing.T) {
	for _, name := range []string{"rules_app", "echo_err_app", "nethttp_app"} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), name+".json")

			code, _, stderr := runMap("--out", out, "./testdata/"+name)
			if code != 0 {
				t.Fatalf("first run exit %d: %s", code, stderr)
			}
			first, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read map: %v", err)
			}

			code, _, stderr = runMap("--out", out, "./testdata/"+name)
			if code != 0 {
				t.Fatalf("second run exit %d: %s", code, stderr)
			}
			second, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read map: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Error("two runs produced different maps")
			}

			golden := filepath.Join("testdata", "golden", name+".json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(golden, first, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden: %v (run UPDATE_GOLDEN=1 to create it)", err)
			}
			if !bytes.Equal(first, want) {
				t.Errorf("map differs from %s\n--- got ---\n%s\n--- want ---\n%s", golden, first, want)
			}
		})
	}
}

// TestGates proves --min-score and --baseline set a non-zero exit.
func TestGates(t *testing.T) {
	out := filepath.Join(t.TempDir(), "map.json")

	code, _, _ := runMap("--out", out, "--min-score", "90", "./testdata/rules_app")
	if code != 1 {
		t.Errorf("--min-score 90: exit %d, want 1", code)
	}
	code, _, _ = runMap("--out", out, "--min-score", "80", "./testdata/rules_app")
	if code != 0 {
		t.Errorf("--min-score 80: exit %d, want 0", code)
	}

	baseline := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baseline, []byte(`{"version":1,"score":100}`), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	code, _, _ = runMap("--out", out, "--baseline", baseline, "./testdata/rules_app")
	if code != 1 {
		t.Errorf("baseline score 100: exit %d, want 1", code)
	}

	low := filepath.Join(t.TempDir(), "low.json")
	if err := os.WriteFile(low, []byte(`{"version":1,"score":50}`), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	code, _, _ = runMap("--out", out, "--baseline", low, "./testdata/rules_app")
	if code != 0 {
		t.Errorf("baseline score 50: exit %d, want 0", code)
	}
}

// TestReportForms proves --all, --entry, and --json each print deterministic bytes.
func TestReportForms(t *testing.T) {
	forms := []struct {
		name string
		args []string
	}{
		{"all", []string{"--all"}},
		{"entry", []string{"--entry", "handleGood"}},
		{"json", []string{"--json"}},
	}
	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "map.json")
			args := append([]string{"--out", out}, form.args...)
			args = append(args, "./testdata/rules_app")

			code, first, stderr := runMap(args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			_, second, _ := runMap(args...)
			if first != second {
				t.Error("two runs printed different bytes")
			}

			golden := filepath.Join("testdata", "golden", "report_"+form.name+".txt")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(golden, []byte(first), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden: %v (run UPDATE_GOLDEN=1)", err)
			}
			if first != string(want) {
				t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", golden, first, want)
			}
		})
	}
}

// TestGates_Strict proves --strict fails on a per-rule regression even when the total
// score did not drop.
func TestGates_Strict(t *testing.T) {
	out := filepath.Join(t.TempDir(), "map.json")
	if code, _, stderr := runMap("--out", out, "./testdata/rules_app"); code != 0 {
		t.Fatalf("run exit %d: %s", code, stderr)
	}
	currentBytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read map: %v", err)
	}
	var current map[string]any
	if err := json.Unmarshal(currentBytes, &current); err != nil {
		t.Fatalf("decode map: %v", err)
	}

	// A baseline with the same score, but no failure on keys.no_denylisted.
	handlers, _ := current["handlers"].([]any)
	for _, entry := range handlers {
		handler, _ := entry.(map[string]any)
		checks, _ := handler["checks"].([]any)
		for _, entry := range checks {
			check, _ := entry.(map[string]any)
			if check["id"] == "keys.no_denylisted" {
				check["pass"] = true
				delete(check, "detail")
			}
		}
	}
	baselineBytes, _ := json.Marshal(current)
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baseline, baselineBytes, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	if code, _, _ := runMap("--out", out, "--baseline", baseline, "./testdata/rules_app"); code != 0 {
		t.Errorf("equal score with a per-rule regression: exit %d, want 0 without --strict", code)
	}
	if code, _, _ := runMap("--out", out, "--strict", "--baseline", baseline, "./testdata/rules_app"); code != 1 {
		t.Errorf("--strict: exit %d, want 1 on a per-rule regression", code)
	}
}
