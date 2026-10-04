package wlog

import (
	"encoding/json"
	"strings"
)

// jsonTreeMaxDepth caps how many times a JSON string may expand inside another.
const jsonTreeMaxDepth = 8

// JSONTree expands every valid JSON object or array string in v into a map or a
// slice. A struct becomes that same tree first. Nested strings expand too, up to
// eight levels. Call it before writing a tool argument or a tool result, so the
// redactor can see a denied key inside the text.
func JSONTree(v any) any {
	return jsonTree(v, 0)
}

func jsonTree(v any, depth int) any {
	if v == nil || depth > jsonTreeMaxDepth {
		return v
	}
	switch x := v.(type) {
	case string:
		return expandJSONText(x, depth)
	case json.RawMessage:
		return expandJSONBytes([]byte(x), depth, v)
	case []byte:
		return expandJSONBytes(x, depth, v)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = jsonTree(item, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = jsonTree(item, depth+1)
		}
		return out
	case bool, int, int64, float64, json.Number:
		return v
	default:
		raw, err := json.Marshal(x)
		if err != nil {
			return v
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return v
		}
		return jsonTree(out, depth+1)
	}
}

// expandJSONText returns s unchanged unless it is a JSON object or array.
func expandJSONText(s string, depth int) any {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s
	}
	expanded := expandJSONBytes([]byte(trimmed), depth, nil)
	if expanded == nil {
		return s
	}
	return expanded
}

// expandJSONBytes parses b when it is a JSON object or array. Anything else
// returns original.
func expandJSONBytes(b []byte, depth int, original any) any {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return original
	}
	var out any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return original
	}
	switch out.(type) {
	case map[string]any, []any:
		return jsonTree(out, depth+1)
	default:
		return original
	}
}
