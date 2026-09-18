// This file tests the OTel preset black box: the golden of each kind, and the semantic
// convention names it writes.
package preset_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/preset"
)

// TestPreset_BET14_OTelGoldens proves that the OTel shape matches a hand-written golden
// for a request, an error, a log, a message, and an LLM event.
func TestPreset_BET14_OTelGoldens(t *testing.T) {
	for _, name := range []string{"request", "error", "log", "message", "llm"} {
		t.Run(name, func(t *testing.T) {
			canonical := readEvent(t, filepath.Join("testdata", "canonical", name+".json"))
			got := normalize(t, preset.OTel().Apply(canonical))
			want := readEvent(t, filepath.Join("testdata", "otel", name+".json"))
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("otel %s =\n%s\nwant\n%s", name, gotJSON, wantJSON)
			}
		})
	}
}

// TestPreset_BET14_SemconvNamesExist proves that every attribute the preset maps itself
// is a name of semconv/v1.43.0. A passthrough key keeps its canonical path, so the test
// leaves it out.
func TestPreset_BET14_SemconvNamesExist(t *testing.T) {
	names := readNames(t, filepath.Join("testdata", "semconv-1.43.0.tsv"), "\t")
	canonical := flattenPaths(syntheticEvent())
	out := normalize(t, preset.OTel().Apply(syntheticEvent()))

	for _, section := range []string{"attributes", "resource"} {
		object, _ := out[section].(map[string]any)
		for name := range object {
			if strings.HasPrefix(name, "gen_ai.") || strings.HasPrefix(name, "http.request.header.") {
				continue
			}
			if canonical[name] {
				continue
			}
			if !names[name] {
				t.Errorf("%s.%s is not a semantic convention name of v1.43.0", section, name)
			}
		}
	}
}

// TestPreset_BET14_GenAINames proves that every gen_ai attribute the preset writes is a
// name of the GenAI conventions.
func TestPreset_BET14_GenAINames(t *testing.T) {
	names := readNames(t, filepath.Join("testdata", "genai-names.txt"), "")
	out := normalize(t, preset.OTel().Apply(syntheticEvent()))
	attrs, _ := out["attributes"].(map[string]any)

	found := 0
	for name := range attrs {
		if !strings.HasPrefix(name, "gen_ai.") {
			continue
		}
		found++
		if !names[name] {
			t.Errorf("%s is not a GenAI convention name", name)
		}
	}
	if found == 0 {
		t.Error("the preset wrote no gen_ai attribute")
	}
}

// syntheticEvent carries one value for every canonical field the OTel table maps, so the
// two name tests cover every rule.
func syntheticEvent() map[string]any {
	return map[string]any{
		"timestamp": "2026-01-01T00:00:00Z", "level": "error", "summary": "op failed",
		"operation": "op", "kind": "request", "outcome": "error", "duration_ms": 1.5,
		"event_id": "0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f",
		"error":    map[string]any{"code": "E1", "message": "m", "type": "*catalog.Error", "stack": "s"},
		"service": map[string]any{
			"name": "shop", "version": "1.2.3", "env": "prod", "instance": "i-1",
		},
		"trace": map[string]any{"trace_id": "t", "span_id": "s"},
		"http": map[string]any{
			"method": "GET", "path": "/x", "status": 200, "scheme": "https",
			"host": "example.com:8443", "protocol": "HTTP/1.1", "client_ip": "203.0.113.7",
			"user_agent": "ua", "bytes_in": 10, "bytes_out": 20,
			"request_headers": map[string]any{"Accept": "text/html"},
		},
		"rpc":       map[string]any{"system": "grpc", "service": "S", "method": "M", "status_code": 0},
		"messaging": map[string]any{"system": "kafka", "destination": "d", "consumer_group": "g", "message_id": "m", "operation": "publish", "partition": 1, "batch_size": 2, "offset": 3},
		"faas":      map[string]any{"name": "f", "version": "1", "trigger": "http", "invocation_id": "i", "cold_start": true, "memory_mb": 128, "region": "r"},
		"user":      map[string]any{"id": "1", "email": "a@b", "name": "n"},
		"llm": map[string]any{
			"provider": "openai", "operation": "chat", "request_model": "m", "response_model": "m2",
			"response_id": "id", "finish_reasons": []any{"stop"}, "input_tokens": 1, "output_tokens": 2,
			"cache_read_input_tokens": 3, "cache_write_input_tokens": 4, "reasoning_tokens": 5,
		},
		"wlog": map[string]any{"schema_version": 2},
	}
}

// flattenPaths returns the dotted paths of every leaf of a canonical event, which are
// the keys a preset passes through unchanged.
func flattenPaths(event map[string]any) map[string]bool {
	out := map[string]bool{}
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		nested, ok := value.(map[string]any)
		if !ok {
			out[prefix] = true
			return
		}
		for key, child := range nested {
			walk(prefix+"."+key, child)
		}
	}
	for key, value := range event {
		walk(key, value)
	}
	return out
}

// readNames reads a name list, skipping a line that starts with #. The name is the first
// column, split by sep when sep is not empty.
func readNames(t *testing.T, path, sep string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	names := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if sep != "" {
			line = strings.SplitN(line, sep, 2)[0]
		}
		names[line] = true
	}
	return names
}

// normalize round-trips a value through JSON, so an int the preset wrote compares equal
// to the float64 a golden file holds.
func normalize(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return out
}
