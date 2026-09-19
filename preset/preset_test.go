// This file tests the preset package black box: the flat shape, the collision rule,
// and the rename wrapper.
package preset_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/preset"
)

// TestPreset_FlatGolden proves that the flat shape matches a hand-written golden for a
// request, an error, and a log event.
func TestPreset_FlatGolden(t *testing.T) {
	for _, tc := range []struct{ name, canonical string }{
		{"request", filepath.Join("testdata", "canonical", "request.json")},
		{"error", filepath.Join("testdata", "canonical", "error.json")},
		{"log", filepath.Join("testdata", "canonical", "log.json")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := preset.Flat().Apply(readEvent(t, tc.canonical))
			want := readEvent(t, filepath.Join("testdata", "flat", tc.name+".json"))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("flat %s =\n %v\nwant\n %v", tc.name, got, want)
			}
		})
	}
}

// TestPreset_FlatCollisionMoves proves that a user key which names a flattened canonical
// key moves to wlog.fields, so it never replaces the canonical field.
func TestPreset_FlatCollisionMoves(t *testing.T) {
	event := map[string]any{
		"timestamp":   "2026-01-01T00:00:00Z",
		"level":       "info",
		"http":        map[string]any{"status": 200},
		"http.status": 999,
	}

	got := preset.Flat().Apply(event)
	if got["http.status"] != 200 {
		t.Errorf("http.status = %v, want the canonical 200", got["http.status"])
	}
	if got["wlog.fields.http.status"] != 999 {
		t.Errorf("wlog.fields.http.status = %v, want the user value 999", got["wlog.fields.http.status"])
	}
}

// TestPreset_RenameBadPairs proves that a good pair renames a key and its lead name,
// and that an odd count or an empty name leaves the preset unchanged and reports
// WLOG_INVALID_CONFIG.
func TestPreset_RenameBadPairs(t *testing.T) {
	renamed := preset.Rename(preset.Flat(), "summary", "msg")
	if !slices.Contains(renamed.Lead(), "msg") {
		t.Errorf("lead = %v, want the new name msg", renamed.Lead())
	}
	got := renamed.Apply(map[string]any{"summary": "hi", "level": "info"})
	if got["msg"] != "hi" {
		t.Errorf("msg = %v, want hi", got["msg"])
	}
	if _, stale := got["summary"]; stale {
		t.Errorf("the old name summary survived: %v", got)
	}

	for _, tc := range []struct {
		name  string
		pairs []string
	}{
		{"odd count", []string{"summary"}},
		{"empty name", []string{"", "msg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := preset.Rename(preset.Flat(), tc.pairs...)
			if got := p.Apply(map[string]any{"summary": "hi"}); got["summary"] != "hi" {
				t.Errorf("a bad pair changed the shape: %v", got)
			}

			var problems []wlog.Problem
			wlog.New(
				wlog.WithOutput(p),
				wlog.OnProblem(func(problem wlog.Problem) { problems = append(problems, problem) }),
			)
			for _, problem := range problems {
				if problem.Code == "WLOG_INVALID_CONFIG" {
					return
				}
			}
			t.Errorf("problems = %+v, want WLOG_INVALID_CONFIG", problems)
		})
	}
}

// TestPreset_ByName proves that a name resolves to the preset of that name, and that an
// unknown name reports false.
func TestPreset_ByName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
		ok   bool
	}{
		{"", "default", true},
		{"default", "default", true},
		{"FLAT", "flat", true},
		{"otel", "otel", true},
		{"ecs", "ecs", true},
		{"datadog", "datadog", true},
		{"yaml", "", false},
	} {
		got, ok := preset.ByName(tc.name)
		if ok != tc.ok {
			t.Errorf("ByName(%q) ok = %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if ok && got.Name() != tc.want {
			t.Errorf("ByName(%q) = %q, want %q", tc.name, got.Name(), tc.want)
		}
	}
}

// readEvent reads one event from a golden file.
func readEvent(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	var event map[string]any
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("%s is not one JSON object: %v", path, err)
	}
	return event
}
