package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

			// The human report goes to stderr, and --json to stdout, so the golden follows the
			// stream the form actually writes.
			code, stdout, stderr := runMap(args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			first := stderr
			if form.name == "json" {
				first = stdout
			}
			_, secondOut, secondErr := runMap(args...)
			second := secondErr
			if form.name == "json" {
				second = secondOut
			}
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

// TestMap_CLI1_ZeroHandlersExit2 proves a run that finds no handler fails instead of reporting a
// perfect score: an app whose routes the analyzer cannot see is not a clean app.
func TestMap_CLI1_ZeroHandlersExit2(t *testing.T) {
	code, stdout, stderr := runMap("./testdata/nohandlers")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "0 handlers found") {
		t.Errorf("stderr = %q, want the handler count", stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout = %q, want nothing on a failed run", stdout)
	}
}

// TestMap_CLI1_HandlerCount proves a successful run reports how many handlers it scored.
func TestMap_CLI1_HandlerCount(t *testing.T) {
	code, _, stderr := runMap("./testdata/nethttp_app")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, stderr)
	}
	if !strings.Contains(stderr, "2 handlers found") {
		t.Errorf("stderr = %q, want the handler count", stderr)
	}
}

// TestMap_CLI5_FailedGateNoWrite proves a failed gate writes no file, and that an --out path
// equal to --baseline is refused, so a failing run can never overwrite the baseline it compares
// against.
func TestMap_CLI5_FailedGateNoWrite(t *testing.T) {
	out := filepath.Join(t.TempDir(), "map.json")

	code, _, _ := runMap("--out", out, "--min-score", "100", "./testdata/config_app")
	if code != 1 {
		t.Fatalf("exit %d, want 1 for a failed gate", code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a failed gate wrote %s", out)
	}

	baseline := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(baseline, []byte(`{"version":1,"score":0}`), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	code, _, stderr := runMap("--out", baseline, "--baseline", baseline, "./testdata/config_app")
	if code != 2 {
		t.Errorf("--out equal to --baseline: exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "baseline") {
		t.Errorf("stderr = %q, want a word about the baseline", stderr)
	}
	after, err := os.ReadFile(baseline)
	if err != nil || string(after) != `{"version":1,"score":0}` {
		t.Errorf("the baseline changed: %s", after)
	}
}

// TestMap_CLI6_FlagsWin proves the config comes from wlog.map.yaml, that the tool's own
// wlog.map.json output is never read as config, and that a flag wins over the file.
func TestMap_CLI6_FlagsWin(t *testing.T) {
	// A wlog.map.json in the pattern's directory is last run's output. Reading it would make the
	// result depend on a leftover file, so the test writes one, gate and all, and expects the run
	// to ignore it.
	leftover := filepath.Join("testdata", "config_app", "wlog.map.json")
	if err := os.WriteFile(leftover, []byte("{\n  \"version\": 2,\n  \"score\": 100,\n  \"min_score\": 100\n}\n"), 0o644); err != nil {
		t.Fatalf("write leftover: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(leftover) })

	code, _, stderr := runMap("--out", filepath.Join(t.TempDir(), "map.json"), "./testdata/config_app")
	if code != 0 {
		t.Errorf("exit %d, want 0: wlog.map.json was read as config: %s", code, stderr)
	}

	// A yaml config is read, and a flag wins over it.
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "wlog.map.yaml")
	if err := os.WriteFile(yamlPath, []byte("min_score: 100\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	code, _, _ = runMap("--out", filepath.Join(dir, "map.json"), "--config", yamlPath, "./testdata/config_app")
	if code != 1 {
		t.Errorf("exit %d, want 1 from the yaml min_score", code)
	}
	code, _, _ = runMap("--out", filepath.Join(dir, "map.json"), "--config", yamlPath, "--min-score", "0", "./testdata/config_app")
	if code != 0 {
		t.Errorf("exit %d, want 0: --min-score 0 must turn the gate off", code)
	}
}

// TestMap_CLI12_OneJSONDocument proves --json puts exactly one JSON document on stdout, with
// every status line on stderr.
func TestMap_CLI12_OneJSONDocument(t *testing.T) {
	code, stdout, stderr := runMap("--json", "--out", filepath.Join(t.TempDir(), "map.json"), "--min-score", "100", "./testdata/config_app")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if !strings.Contains(stderr, "gate failed") {
		t.Errorf("stderr = %q, want the gate status", stderr)
	}
}

// TestMap_PAR28_NoColorColumns proves NO_COLOR turns colors off and COLUMNS wraps the text
// report.
func TestMap_PAR28_NoColorColumns(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLUMNS", "40")

	_, stdout, stderr := runMap("--all", "--out", filepath.Join(t.TempDir(), "map.json"), "./testdata/rules_app")
	text := stdout + stderr
	if strings.Contains(text, "\x1b[") {
		t.Errorf("NO_COLOR was set and the report still carries an escape code:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if len(line) > 40 {
			t.Errorf("a line is %d characters, want at most 40:\n%s", len(line), line)
		}
	}
}

// TestMap_CLI21_TextReportGolden proves the command's text report lists every failing handler
// with its file:line, rule, and fix, then FIX FIRST with the projected score, and that two runs
// print the same bytes.
func TestMap_CLI21_TextReportGolden(t *testing.T) {
	out := filepath.Join(t.TempDir(), "map.json")
	code, _, report := runMap("--out", out, "./testdata/rules_app")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(report, "FIX FIRST") || !strings.Contains(report, "projected score") {
		t.Errorf("the report has no FIX FIRST section:\n%s", report)
	}
	if !strings.Contains(report, "main.go:") {
		t.Errorf("the report names no file:line:\n%s", report)
	}

	golden := filepath.Join("testdata", "golden", "report_text_cli.txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(report), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v (run UPDATE_GOLDEN=1)", err)
	}
	if report != string(want) {
		t.Errorf("the report differs from %s\n--- got ---\n%s\n--- want ---\n%s", golden, report, want)
	}
}

// TestMap_PAR31_GitBaseline proves --baseline git:<ref> reads the map at that revision through
// git show, and that --no-write writes no file.
func TestMap_PAR31_GitBaseline(t *testing.T) {
	// The fixture directory holds a committed map that claims a perfect score, so reading it at
	// HEAD must fail the gate. The run is a normal one from the module directory, which is where
	// the git commands run too.
	// A tracked map at HEAD: the golden for rules_app scores higher than the echo_err_app
	// fixture, so reading it at HEAD must fail this run's gate.
	baseline := filepath.Join("testdata", "golden", "rules_app.json")

	code, _, stderr := runMap("--out", baseline, "--no-write", "--baseline", "git:HEAD", "./testdata/echo_err_app")
	if code != 1 {
		t.Errorf("exit %d, want 1: the committed baseline scores higher than the app: %s", code, stderr)
	}
	if !strings.Contains(stderr, "git:HEAD") {
		t.Errorf("stderr = %q, want the revision it compared against", stderr)
	}

	// A revision that does not hold the file is reported, not ignored.
	if code, _, stderr := runMap("--out", baseline, "--no-write", "--baseline", "git:nosuchref", "./testdata/echo_err_app"); code != 2 {
		t.Errorf("a missing revision: exit %d, want 2: %s", code, stderr)
	}

	// --no-write leaves the output file alone.
	noWrite := filepath.Join(t.TempDir(), "untouched.json")
	if code, _, stderr := runMap("--out", noWrite, "--no-write", "./testdata/rules_app"); code != 0 {
		t.Errorf("--no-write run exited %d, want 0: %s", code, stderr)
	}
	if _, err := os.Stat(noWrite); !os.IsNotExist(err) {
		t.Errorf("--no-write wrote %s", noWrite)
	}
}
