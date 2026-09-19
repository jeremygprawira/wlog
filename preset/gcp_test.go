// This file tests the GCP and EMF presets black box: the golden of each kind, the trace
// name without a project, and the EMF dimension rule.
package preset_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jeremygprawira/wlog/preset"
)

// gcpTestProject is the project the goldens use, so the trace name is stable.
const gcpTestProject = "shop-project"

// TestPreset_GCPGoldens proves that the GCP shape matches a hand-written golden for a
// request, an error, and a log event.
func TestPreset_GCPGoldens(t *testing.T) {
	for _, name := range []string{"request", "error", "log"} {
		t.Run(name, func(t *testing.T) {
			canonical := readEvent(t, filepath.Join("testdata", "canonical", name+".json"))
			got := normalize(t, preset.GCP(preset.GCPProject(gcpTestProject)).Apply(canonical))
			want := readEvent(t, filepath.Join("testdata", "gcp", name+".json"))
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("gcp %s =\n%s\nwant\n%s", name, gotJSON, wantJSON)
			}
		})
	}
}

// TestPreset_GCPTraceWithoutProject proves that a preset with no project writes the bare
// trace id, which Cloud Logging accepts.
func TestPreset_GCPTraceWithoutProject(t *testing.T) {
	canonical := readEvent(t, filepath.Join("testdata", "canonical", "request.json"))
	got := preset.GCP(preset.GCPProject("")).Apply(canonical)
	want := "4bf92f3577b34da6a3ce929d0e0e4736"
	if got["logging.googleapis.com/trace"] != want {
		t.Errorf("trace = %v, want the bare id %q", got["logging.googleapis.com/trace"], want)
	}
}

// TestPreset_EMFGoldens proves that the EMF shape matches a hand-written golden for a
// request, and that a log event carries no _aws object.
func TestPreset_EMFGoldens(t *testing.T) {
	for _, name := range []string{"request", "log"} {
		t.Run(name, func(t *testing.T) {
			canonical := readEvent(t, filepath.Join("testdata", "canonical", name+".json"))
			got := normalize(t, preset.EMF().Apply(canonical))
			want := readEvent(t, filepath.Join("testdata", "emf", name+".json"))
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("emf %s =\n%s\nwant\n%s", name, gotJSON, wantJSON)
			}
			if name == "log" {
				if _, ok := got["_aws"]; ok {
					t.Errorf("a log event carries _aws: %v", got)
				}
			}
		})
	}
}

// TestPreset_EMFMissingDimension proves that a dimension set with a missing key is left
// out, and that one empty set stands in when no set is left.
func TestPreset_EMFMissingDimension(t *testing.T) {
	event := map[string]any{"kind": "work", "service": map[string]any{"name": "shop"}}

	only := preset.EMF(preset.EMFDimensions([]string{"service.name", "operation"}))
	if got := dimensions(t, only.Apply(event)); !reflect.DeepEqual(got, []any{[]any{}}) {
		t.Errorf("dimensions = %v, want one empty set", got)
	}

	two := preset.EMF(preset.EMFDimensions(
		[]string{"service.name", "operation"},
		[]string{"service.name"},
	))
	if got := dimensions(t, two.Apply(event)); !reflect.DeepEqual(got, []any{[]any{"service.name"}}) {
		t.Errorf("dimensions = %v, want one set named service.name", got)
	}
}

// dimensions reads the dimension sets of the _aws directive.
func dimensions(t *testing.T, event map[string]any) []any {
	t.Helper()
	aws, ok := event["_aws"].(map[string]any)
	if !ok {
		t.Fatalf("the event carries no _aws object: %v", event)
	}
	directives, ok := aws["CloudWatchMetrics"].([]any)
	if !ok || len(directives) != 1 {
		t.Fatalf("CloudWatchMetrics = %v, want one directive", aws["CloudWatchMetrics"])
	}
	directive, _ := directives[0].(map[string]any)
	sets, _ := directive["Dimensions"].([]any)
	return sets
}
