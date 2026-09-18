// This file holds the flat preset: every nested object becomes dotted keys at every
// depth, which suits a backend that reads only root keys.
package preset

import (
	"github.com/jeremygprawira/wlog"
)

// flatLead is the top-level order the flat preset prints first.
var flatLead = []string{
	"timestamp", "level", "summary", "operation", "kind", "outcome", "duration_ms", "message",
}

// Flat returns the preset that writes every nested object as dotted keys at every depth,
// such as http.status and error.code. An array stays an array, and an object inside an
// array stays nested.
func Flat() wlog.OutputPreset { return flatPreset{} }

// flatPreset writes one level of dotted keys.
type flatPreset struct{}

// Name returns "flat".
func (flatPreset) Name() string { return "flat" }

// Lead returns the top-level keys the writer prints first.
func (flatPreset) Lead() []string { return flatLead }

// Apply returns the event as dotted keys.
//
// A nested object flattens first. A key that holds no object then joins it, unless its
// name already names a flattened key, which means a user key would replace a canonical
// field. That key moves to wlog.fields.<key> instead.
func (flatPreset) Apply(event map[string]any) map[string]any {
	out := make(map[string]any, len(event))
	for key, value := range event {
		if _, nested := value.(map[string]any); nested {
			flatten(out, key, value)
		}
	}
	for key, value := range event {
		if _, nested := value.(map[string]any); nested {
			continue
		}
		if _, taken := out[key]; taken {
			out["wlog.fields."+key] = value
			continue
		}
		out[key] = value
	}
	return out
}

// flatten writes value under prefix, and descends into every nested object. An array or
// a scalar is written as it is, so a dotted key holds the array.
func flatten(out map[string]any, prefix string, value any) {
	nested, ok := value.(map[string]any)
	if !ok {
		out[prefix] = value
		return
	}
	for key, child := range nested {
		flatten(out, prefix+"."+key, child)
	}
}
