// This file tests the ECS and Datadog presets black box: the golden of each kind, the
// client address rule, and the fields Datadog never writes.
package preset_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jeremygprawira/wlog/preset"
)

// TestPreset_ECSGoldens proves that the ECS shape matches a hand-written golden for a
// request, an error, and a log event.
func TestPreset_ECSGoldens(t *testing.T) {
	for _, name := range []string{"request", "error", "log"} {
		t.Run(name, func(t *testing.T) {
			canonical := readEvent(t, filepath.Join("testdata", "canonical", name+".json"))
			got := normalize(t, preset.ECS().Apply(canonical))
			want := readEvent(t, filepath.Join("testdata", "ecs", name+".json"))
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("ecs %s =\n%s\nwant\n%s", name, gotJSON, wantJSON)
			}
		})
	}
}

// TestPreset_ECSClientIP proves that a client value which netip.ParseAddr accepts becomes
// client.ip, and that every other value becomes client.address.
func TestPreset_ECSClientIP(t *testing.T) {
	for _, tc := range []struct {
		value string
		key   string
	}{
		{"203.0.113.7", "ip"},
		{"not-an-address", "address"},
	} {
		event := map[string]any{"http": map[string]any{"client_ip": tc.value}}
		got := preset.ECS().Apply(event)
		client, _ := got["client"].(map[string]any)
		if client[tc.key] != tc.value {
			t.Errorf("client %s = %v, want %q for %q", tc.key, client[tc.key], tc.value, tc.value)
		}
	}
}

// TestPreset_DatadogGoldens proves that the Datadog shape matches a hand-written golden
// for a request, an error, and a log event, and that it never writes host.
func TestPreset_DatadogGoldens(t *testing.T) {
	for _, name := range []string{"request", "error", "log"} {
		t.Run(name, func(t *testing.T) {
			canonical := readEvent(t, filepath.Join("testdata", "canonical", name+".json"))
			got := normalize(t, preset.Datadog().Apply(canonical))
			want := readEvent(t, filepath.Join("testdata", "datadog", name+".json"))
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				wantJSON, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("datadog %s =\n%s\nwant\n%s", name, gotJSON, wantJSON)
			}
			if path := findKey(got, "host"); path != "" {
				t.Errorf("datadog %s writes host at %s", name, path)
			}
		})
	}
}

// findKey returns the path of the first key with this name, or an empty string.
func findKey(value any, name string) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == name {
				return key
			}
			if found := findKey(child, name); found != "" {
				return key + "." + found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findKey(child, name); found != "" {
				return found
			}
		}
	}
	return ""
}
