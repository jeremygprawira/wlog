// This file holds the shared conformance harness: the recorder a suite reads back, and
// the two helpers that turn one recorded event into something two runs can compare.
package conformance

import (
	"fmt"
	"net"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Recorder is what a suite reads back: the events an adapter emitted, and the problems the
// Logger reported. A suite takes one from its adapter factory, so the same scenarios run
// against a real adapter and a fake one.
type Recorder interface {
	Events() []map[string]any
	Problems() []wlog.Problem
}

// normalizeKeys are the keys whose value changes between two runs of one scenario.
var normalizeKeys = map[string]bool{
	"timestamp": true, "event_id": true,
	"trace_id": true, "span_id": true, "parent_span_id": true,
}

// durationText matches the duration text of a summary, such as 0.84ms, 12.8ms, or 1.24s.
var durationText = regexp.MustCompile(`\d+(?:\.\d+)?(?:ms|s)\b`)

// Normalize returns a copy of an event with every value that changes between runs removed,
// so two runs of one scenario compare equal.
//
// It removes the timestamp, the event id, every trace and span id, the service instance,
// and every key that ends in _ms at any depth. It replaces the duration text inside the
// summary with {d}, and it removes the port from a host, which a test server picks.
func Normalize(event map[string]any) map[string]any {
	out, _ := normalizeValue(event, "").(map[string]any)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

// normalizeValue copies one value, and drops what a later run would change.
func normalizeValue(value any, parent string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if normalizeKeys[key] || strings.HasSuffix(key, "_ms") {
				continue
			}
			if parent == "service" && key == "instance" {
				continue
			}
			out[key] = normalizeValue(child, key)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = normalizeValue(child, parent)
		}
		return out
	case string:
		return normalizeString(typed, parent)
	default:
		return value
	}
}

// normalizeString replaces the run-varying text of one string value.
func normalizeString(text, key string) string {
	switch key {
	case "summary":
		return durationText.ReplaceAllString(text, "{d}")
	case "host":
		return withoutPort(text)
	default:
		return text
	}
}

// withoutPort removes the port from an address, and keeps the value when it carries none.
func withoutPort(host string) string {
	address, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return address
}

// Diff returns the readable difference between two normalized events, one line per field.
// An empty string means the two events match.
func Diff(want, got map[string]any) string {
	wantFlat := flattenEvent(want, "")
	gotFlat := flattenEvent(got, "")
	keys := make([]string, 0, len(wantFlat)+len(gotFlat))
	for key := range wantFlat {
		keys = append(keys, key)
	}
	for key := range gotFlat {
		if _, both := wantFlat[key]; !both {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var lines []string
	for _, key := range keys {
		wantValue, inWant := wantFlat[key]
		gotValue, inGot := gotFlat[key]
		switch {
		case inWant && !inGot:
			lines = append(lines, fmt.Sprintf("- %s: want %v", key, wantValue))
		case !inWant && inGot:
			lines = append(lines, fmt.Sprintf("+ %s: got %v", key, gotValue))
		case !reflect.DeepEqual(wantValue, gotValue):
			lines = append(lines, fmt.Sprintf("! %s: want %v, got %v", key, wantValue, gotValue))
		}
	}
	return strings.Join(lines, "\n")
}

// flattenEvent returns every leaf of an event under its dotted path, so a difference names
// one field rather than one whole group.
func flattenEvent(event map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for key, value := range event {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if nested, ok := value.(map[string]any); ok {
			for nestedPath, leaf := range flattenEvent(nested, path) {
				out[nestedPath] = leaf
			}
			continue
		}
		out[path] = value
	}
	return out
}
