package audit

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Diff compares two objects field by field and returns only what changed, as a nested
// tree of {"field": {"from": x, "to": y}} entries. A nested object stays nested, so a
// path denylist entry such as changes.user.creds.nik matches the diff, and the redactor
// masks the value it names.
//
// It walks a struct through its JSON tags, so it does not need to know the type, and it
// returns an empty map for two equal values. It returns an error when either side is not
// an object, because a slice or a scalar has no fields to compare: an empty diff would
// read as "nothing changed".
//
// Diff compares the JSON view of both values, so a field json.Marshal skips, such as an
// unexported one, is outside the comparison.
//
// Diff only builds a map. It never writes to the event, so the normal redacted path
// masks every value the denylist denies.
func Diff(before, after any) (map[string]any, error) {
	left, err := toTree(before)
	if err != nil {
		return nil, err
	}
	right, err := toTree(after)
	if err != nil {
		return nil, err
	}
	return diffObjects(left, right), nil
}

// toTree renders an object as a JSON tree. A struct goes through its tags, and a map
// passes through. nil is an empty object. Anything else is an error.
func toTree(value any) (map[string]any, error) {
	switch v := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return v, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("audit: Diff: %w", err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, fmt.Errorf("audit: Diff needs two objects, got %T", value)
	}
	return object, nil
}

// diffObjects compares two objects and returns the changed keys, nested where both sides
// hold an object. Two equal values give an empty map.
func diffObjects(before, after map[string]any) map[string]any {
	out := map[string]any{}
	for key := range unionKeys(before, after) {
		left, leftOK := before[key]
		right, rightOK := after[key]
		if leftOK == rightOK && reflect.DeepEqual(left, right) {
			continue
		}

		leftMap, leftIsMap := left.(map[string]any)
		rightMap, rightIsMap := right.(map[string]any)
		if leftIsMap && rightIsMap {
			if nested := diffObjects(leftMap, rightMap); len(nested) > 0 {
				out[key] = nested
			}
			continue
		}
		// A value that changed type, such as a number that became an object, is one
		// change at its own key. Descending into it would report nothing at all.
		out[key] = map[string]any{"from": left, "to": right}
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
