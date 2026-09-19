// Package preset holds the output presets of wlog: the JSON shapes a writer can print
// for a backend. A preset changes the printed line only, so a drain always receives the
// canonical event.
//
// Core hands Apply a copy of the event, so a preset may change what it is given. A
// preset with no Lead key keeps the fixed key order of the default shape.
package preset

import (
	"fmt"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Default returns the preset that prints the canonical event in the fixed key order of
// the default shape.
func Default() wlog.OutputPreset { return defaultPreset{} }

// ByName returns the preset that name selects, which is how WLOG_OUTPUT resolves. The
// name is matched without case.
func ByName(name string) (wlog.OutputPreset, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "default":
		return Default(), true
	case "flat":
		return Flat(), true
	case "otel":
		return OTel(), true
	case "ecs":
		return ECS(), true
	case "datadog":
		return Datadog(), true
	case "gcp":
		return GCP(), true
	case "emf":
		return EMF(), true
	}
	return nil, false
}

// Rename returns p with top-level keys renamed after Apply. fromTo holds pairs of the
// old name and the new name.
//
// The lead order uses the new name. An odd count of names or an empty name leaves p
// unchanged, and WithOutput then reports WLOG_INVALID_CONFIG.
func Rename(p wlog.OutputPreset, fromTo ...string) wlog.OutputPreset {
	renamed := renamed{inner: p, names: map[string]string{}}
	if len(fromTo)%2 != 0 {
		renamed.err = fmt.Errorf("Rename needs pairs of names, and got %d", len(fromTo))
		return renamed
	}
	for i := 0; i < len(fromTo); i += 2 {
		if fromTo[i] == "" || fromTo[i+1] == "" {
			renamed.err = fmt.Errorf("Rename got an empty name")
			return renamed
		}
		renamed.names[fromTo[i]] = fromTo[i+1]
	}
	return renamed
}

// renamed wraps a preset and renames top-level keys of the map it returns.
type renamed struct {
	inner wlog.OutputPreset
	names map[string]string
	err   error // set when the pairs were bad, so WithOutput reports the config
}

// Name returns the name of the preset it wraps.
func (r renamed) Name() string { return r.inner.Name() }

// Lead returns the lead keys of the wrapped preset, with the new names.
func (r renamed) Lead() []string {
	lead := r.inner.Lead()
	if r.err != nil {
		return lead
	}
	out := make([]string, 0, len(lead))
	for _, key := range lead {
		if to, renamed := r.names[key]; renamed {
			key = to
		}
		out = append(out, key)
	}
	return out
}

// Apply returns the shaped event of the wrapped preset with the named keys renamed.
func (r renamed) Apply(event map[string]any) map[string]any {
	out := r.inner.Apply(event)
	if r.err != nil {
		return out
	}
	for from, to := range r.names {
		value, present := out[from]
		if !present {
			continue
		}
		delete(out, from)
		out[to] = value
	}
	return out
}

// ConfigError returns the bad configuration of this preset, or nil.
func (r renamed) ConfigError() error { return r.err }

// defaultPreset prints the canonical event in the fixed key order.
type defaultPreset struct{}

// Name returns "default".
func (defaultPreset) Name() string { return "default" }

// Lead returns no key, which keeps the fixed key order of the default shape.
func (defaultPreset) Lead() []string { return nil }

// Apply returns a new map with the same keys.
func (defaultPreset) Apply(event map[string]any) map[string]any {
	out := make(map[string]any, len(event))
	for key, value := range event {
		out[key] = value
	}
	return out
}

// pathValue reads the value at a dotted path, and reports whether it is present.
func pathValue(event map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var current any = event
	for _, part := range parts {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// setPath writes value at a dotted path, and creates the objects the path needs.
func setPath(out map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	object := out
	for _, part := range parts[:len(parts)-1] {
		next, ok := object[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			object[part] = next
		}
		object = next
	}
	object[parts[len(parts)-1]] = value
}

// deletePath removes the value at a dotted path, and the empty objects it leaves.
func deletePath(out map[string]any, path string) {
	parts := strings.Split(path, ".")
	parents := make([]map[string]any, 0, len(parts))
	object := out
	for _, part := range parts[:len(parts)-1] {
		next, ok := object[part].(map[string]any)
		if !ok {
			return
		}
		parents = append(parents, object)
		object = next
	}
	delete(object, parts[len(parts)-1])
	// A parent that lost its last child goes too, so the output holds no empty object.
	for i := len(parents) - 1; i >= 0; i-- {
		if len(object) == 0 {
			delete(parents[i], parts[i])
		}
		object = parents[i]
	}
}

// movePath moves the value at one dotted path to another.
func movePath(out map[string]any, from, to string) {
	value, ok := pathValue(out, from)
	if !ok {
		return
	}
	deletePath(out, from)
	setPath(out, to, value)
}

// stringOf reads a string value, and treats a missing or wrong-typed value as "".
func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// textOf renders a string or a number as text, which a label or a dimension needs.
func textOf(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return numberString(value)
}

// floatValue reads a number, and treats a missing or wrong-typed value as zero.
func floatValue(value any) float64 {
	switch number := value.(type) {
	case int:
		return float64(number)
	case int64:
		return float64(number)
	case float64:
		return number
	default:
		return 0
	}
}
