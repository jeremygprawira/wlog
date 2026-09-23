// This file maps the redacted event to the attributes of one log record through the otel
// output preset, so a log record and a span name the same field the same way.
package wlogotellog

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"

	"go.opentelemetry.io/otel/attribute"

	"github.com/jeremygprawira/wlog/preset"
)

// recordAttributes returns the attributes of one record: the attributes object of the
// otel preset, and the resource keys when the caller asked for them.
func recordAttributes(event map[string]any, service bool) []attribute.KeyValue {
	record := preset.OTel().Apply(event)
	out := []attribute.KeyValue{}
	if attrs, ok := record["attributes"].(map[string]any); ok {
		out = append(out, attributesOf(attrs)...)
	}
	if service {
		if resource, ok := record["resource"].(map[string]any); ok {
			out = append(out, attributesOf(resource)...)
		}
	}
	return out
}

// attributesOf converts one flat map to attributes in a stable order.
func attributesOf(values map[string]any) []attribute.KeyValue {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]attribute.KeyValue, 0, len(names))
	for _, name := range names {
		out = append(out, attributeOf(name, values[name]))
	}
	return out
}

// attributeOf converts one event value to an OTel attribute. A slice of one scalar type
// becomes a typed slice, a slice of objects becomes a slice of map values, and any other
// slice becomes one JSON string.
func attributeOf(name string, value any) attribute.KeyValue {
	switch v := value.(type) {
	case nil:
		return attribute.KeyValue{Key: attribute.Key(name)}
	case string:
		return attribute.String(name, v)
	case bool:
		return attribute.Bool(name, v)
	case int:
		return attribute.Int(name, v)
	case int64:
		return attribute.Int64(name, v)
	case uint64:
		if v <= math.MaxInt64 {
			return attribute.Int64(name, int64(v))
		}
		return attribute.String(name, strconv.FormatUint(v, 10))
	case float64:
		return attribute.Float64(name, v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return attribute.Int64(name, i)
		}
		if f, err := v.Float64(); err == nil {
			return attribute.Float64(name, f)
		}
		return attribute.String(name, v.String())
	case map[string]any:
		return attribute.Map(name, attributesOf(v)...)
	case []any:
		return sliceAttribute(name, v)
	default:
		return attribute.String(name, jsonText(value))
	}
}

// sliceAttribute converts a slice to one attribute.
func sliceAttribute(name string, values []any) attribute.KeyValue {
	if len(values) == 0 {
		return attribute.Slice(name)
	}
	if _, ok := values[0].(map[string]any); ok {
		items := make([]attribute.Value, 0, len(values))
		for _, value := range values {
			object, ok := value.(map[string]any)
			if !ok {
				return attribute.String(name, jsonText(values))
			}
			items = append(items, attribute.MapValue(attributesOf(object)...))
		}
		return attribute.Slice(name, items...)
	}
	switch values[0].(type) {
	case string:
		out := make([]string, len(values))
		for i, value := range values {
			text, ok := value.(string)
			if !ok {
				return attribute.String(name, jsonText(values))
			}
			out[i] = text
		}
		return attribute.StringSlice(name, out)
	case bool:
		out := make([]bool, len(values))
		for i, value := range values {
			b, ok := value.(bool)
			if !ok {
				return attribute.String(name, jsonText(values))
			}
			out[i] = b
		}
		return attribute.BoolSlice(name, out)
	case float64:
		out := make([]float64, len(values))
		for i, value := range values {
			f, ok := value.(float64)
			if !ok {
				return attribute.String(name, jsonText(values))
			}
			out[i] = f
		}
		return attribute.Float64Slice(name, out)
	default:
		out := make([]int64, len(values))
		for i, value := range values {
			n, ok := intValue(value)
			if !ok {
				return attribute.String(name, jsonText(values))
			}
			out[i] = n
		}
		return attribute.Int64Slice(name, out)
	}
}

// intValue reads an integer from the types an event holds.
func intValue(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case uint64:
		if v <= math.MaxInt64 {
			return int64(v), true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}

// jsonText renders a value that OTel cannot hold as one JSON string.
func jsonText(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(body)
}

// textOf reads a string from a value.
func textOf(value any) string {
	text, _ := value.(string)
	return text
}
