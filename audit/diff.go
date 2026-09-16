package audit

import (
	"encoding/json"
	"reflect"
)

// Diff compares two values field by field and returns only what changed, as
// {"field": {"from": x, "to": y}}. It walks a struct through its JSON tags, so it does
// not need to know the type, and it returns an empty map for two equal values. A
// nested map reports a dotted path, such as user.plan.
//
// Diff only builds a map. It never writes to the event, so the normal redacted path
// masks a field the denylist denies.
func Diff(before, after any) map[string]any {
	return diffMaps(toTree(before), toTree(after), "")
}

// toTree renders a value as a JSON object. A struct goes through its tags. A value that
// is already a map passes through, and anything else becomes an empty object.
func toTree(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if object, ok := value.(map[string]any); ok {
		return object
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return map[string]any{}
	}
	return object
}

// diffMaps compares two objects under one dotted prefix.
func diffMaps(before, after map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for key := range unionKeys(before, after) {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		left, leftOK := before[key]
		right, rightOK := after[key]

		leftMap, leftIsMap := left.(map[string]any)
		rightMap, rightIsMap := right.(map[string]any)
		if leftIsMap || rightIsMap {
			if !leftIsMap {
				leftMap = map[string]any{}
			}
			if !rightIsMap {
				rightMap = map[string]any{}
			}
			for nestedKey, value := range diffMaps(leftMap, rightMap, path) {
				out[nestedKey] = value
			}
			continue
		}

		if leftOK == rightOK && reflect.DeepEqual(left, right) {
			continue
		}
		out[path] = map[string]any{"from": left, "to": right}
	}
	return out
}

// unionKeys returns every key either map holds.
func unionKeys(a, b map[string]any) map[string]struct{} {
	keys := make(map[string]struct{}, len(a)+len(b))
	for key := range a {
		keys[key] = struct{}{}
	}
	for key := range b {
		keys[key] = struct{}{}
	}
	return keys
}
