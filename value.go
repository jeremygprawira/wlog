// This file owns the values that an event carries.
//
// Every write into an event copies its value into a tree that wlog owns, because
// a caller must never be able to change an event after the fact, and a drain must
// never see a map that another goroutine still writes. The tree holds only nil,
// bool, string, int64, uint64, float64, json.Number, map[string]any, and []any.
//
// The copy also converts the values that JSON cannot hold directly: an error
// becomes its message, a duration becomes milliseconds, a time becomes RFC 3339,
// and a NaN or an infinity becomes its name as a string. A value that cannot be
// copied becomes "[unencodable: <type>]", so one bad field never loses the rest
// of the event.
//
// The copy runs before the event lock is taken. A MarshalJSON method that logs
// through wlog on the same event therefore finishes instead of deadlocking.
package wlog

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// maxValueDepth caps how deep the copy walks. A deeper value becomes
// "[truncated: depth]", because a hostile or a cyclic value must not grow the
// event without bound.
const maxValueDepth = 16

// copyValue returns a copy of v that wlog owns.
func copyValue(v any) any {
	return copyAt(v, 1)
}

// copyAt copies one value at a known depth.
//
// It recovers from a panic in a method it calls, because a user type may panic
// in MarshalJSON, in TextMarshaler, or in Error, and a panic in a logging call
// never reaches the caller.
func copyAt(v any, depth int) (out any) {
	if depth > maxValueDepth {
		return "[truncated: depth]"
	}
	defer func() {
		if r := recover(); r != nil {
			out = "[unencodable: " + typeName(v) + "]"
		}
	}()

	switch t := v.(type) {
	case nil:
		return nil
	case bool, string, int64, uint64:
		return t
	case json.Number:
		return numericValue(t)
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case uint:
		return uint64(t)
	case uint8:
		return uint64(t)
	case uint16:
		return uint64(t)
	case uint32:
		return uint64(t)
	case uintptr:
		return uint64(t)
	case float64:
		return finiteNumber(t)
	case float32:
		return finiteNumber(float64(t))
	case time.Duration:
		return float64(t) / float64(time.Millisecond)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case error:
		return errorMessage(t)
	case json.RawMessage:
		return decodeJSON(t, depth)
	case []byte:
		return fmt.Sprintf("[binary: %d bytes]", len(t))
	case json.Marshaler:
		// A Marshaler decides its own JSON, so decode it and copy the result.
		raw, err := t.MarshalJSON()
		if err != nil {
			return "[unencodable: " + typeName(v) + "]"
		}
		return decodeJSON(raw, depth)
	case encoding.TextMarshaler:
		text, err := t.MarshalText()
		if err != nil {
			return "[unencodable: " + typeName(v) + "]"
		}
		if !utf8.Valid(text) {
			return fmt.Sprintf("[binary: %d bytes]", len(text))
		}
		return string(text)
	case map[string]any:
		return copyMap(t, depth)
	case map[string]string:
		return copyStringMap(t, depth)
	case []any:
		return copySlice(t, depth)
	}
	return copyReflect(reflect.ValueOf(v), depth)
}

// copyMap copies a map of any values.
func copyMap(m map[string]any, depth int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = copyAt(v, depth+1)
	}
	return out
}

// copyStringMap copies a map of strings, which JSON renders as an object.
func copyStringMap(m map[string]string, depth int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// copySlice copies a slice of any values.
func copySlice(s []any, depth int) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = copyAt(v, depth+1)
	}
	return out
}

// copyReflect copies a value that the fast paths above do not name: a typed map,
// slice, array, struct, or pointer.
func copyReflect(rv reflect.Value, depth int) any {
	if !rv.IsValid() {
		return nil
	}
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return copyAt(rv.Elem().Interface(), depth)
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if rv.Type() == reflect.TypeOf(time.Duration(0)) {
			return float64(rv.Int()) / float64(time.Millisecond)
		}
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint()
	case reflect.Float32, reflect.Float64:
		return finiteNumber(rv.Float())
	case reflect.Slice, reflect.Array:
		return copyReflectSlice(rv, depth)
	case reflect.Map:
		return copyReflectMap(rv, depth)
	case reflect.Struct:
		return copyStruct(rv, depth)
	}
	// A function, a channel, or a complex number has no JSON form.
	return "[unencodable: " + typeName(rv.Interface()) + "]"
}

// copyReflectSlice copies a typed slice or array. A byte slice is binary data,
// and a rune slice is text.
func copyReflectSlice(rv reflect.Value, depth int) any {
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		return fmt.Sprintf("[binary: %d bytes]", rv.Len())
	}
	out := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, copyAt(rv.Index(i).Interface(), depth+1))
	}
	return out
}

// copyReflectMap copies a typed map. JSON requires a string key, so a map that
// holds another key type becomes unencodable.
func copyReflectMap(rv reflect.Value, depth int) any {
	if rv.IsNil() {
		return nil
	}
	if rv.Type().Key().Kind() != reflect.String {
		return "[unencodable: " + typeName(rv.Interface()) + "]"
	}
	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		out[iter.Key().String()] = copyAt(iter.Value().Interface(), depth+1)
	}
	return out
}

// field is one struct field that the copy keeps, under its JSON name.
type field struct {
	name      string
	index     []int
	omitEmpty bool
	asString  bool
}

// copyStruct copies a struct by the rules of encoding/json: the json tag names
// the field, "-" drops it, omitempty drops a zero value, and an embedded struct
// without a tag of its own contributes its fields to the parent object.
func copyStruct(rv reflect.Value, depth int) map[string]any {
	out := map[string]any{}
	for _, f := range visibleFields(rv.Type()) {
		fv := rv.FieldByIndex(f.index)
		if !fv.IsValid() || !fv.CanInterface() {
			continue
		}
		if f.omitEmpty && fv.IsZero() {
			continue
		}
		switch {
		case f.asString:
			out[f.name] = stringify(fv)
		case fv.Kind() == reflect.Interface && fv.IsNil():
			out[f.name] = nil
		default:
			out[f.name] = copyAt(fv.Interface(), depth+1)
		}
	}
	return out
}

// visibleFields returns the fields of a struct type that JSON keeps, in order.
//
// An embedded struct without a tag of its own contributes its own fields to the
// parent, which is how encoding/json flattens an embedded type. A field that a
// nearer level also names wins, so the rule matches the encoding.
func visibleFields(typ reflect.Type) []field {
	var out []field
	seen := map[string]bool{}
	var walk func(t reflect.Type, prefix []int)
	walk = func(t reflect.Type, prefix []int) {
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			name, opts, tagged := parseJSONTag(sf)
			if name == "-" {
				continue
			}
			// An embedded field of an unexported type still contributes its
			// exported fields, which is how encoding/json reads it.
			if !sf.IsExported() && !sf.Anonymous {
				continue
			}
			index := append(append([]int{}, prefix...), i)
			if sf.Anonymous && !tagged && sf.Type.Kind() == reflect.Struct {
				walk(sf.Type, index)
				continue
			}
			if name == "" {
				name = sf.Name
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, field{
				name:      name,
				index:     index,
				omitEmpty: opts["omitempty"],
				asString:  opts["string"],
			})
		}
	}
	walk(typ, nil)
	return out
}

// parseJSONTag returns the JSON name of a field, its options, and whether the
// tag named it.
func parseJSONTag(sf reflect.StructField) (name string, opts map[string]bool, tagged bool) {
	opts = map[string]bool{}
	tag, ok := sf.Tag.Lookup("json")
	if !ok {
		return "", opts, false
	}
	parts := strings.Split(tag, ",")
	if parts[0] != "" {
		name = parts[0]
		tagged = true
	}
	for _, opt := range parts[1:] {
		if opt != "" {
			opts[opt] = true
		}
	}
	return name, opts, tagged
}

// stringify renders a field that carries the ",string" option. JSON quotes the
// value, so the result is always a string.
func stringify(rv reflect.Value) string {
	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return fmt.Sprintf("%t", rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", rv.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", rv.Float())
	}
	return "[unencodable: " + typeName(rv.Interface()) + "]"
}

// numericValue turns a JSON number into the smallest type that keeps every one
// of its digits: an int64, then a uint64, then a float64. A number that fits
// none of them keeps its text, because a float would round it.
func numericValue(n json.Number) any {
	if i, err := n.Int64(); err == nil {
		return i
	}
	if u, err := strconv.ParseUint(n.String(), 10, 64); err == nil {
		return u
	}
	if f, err := n.Float64(); err == nil {
		return finiteNumber(f)
	}
	return n
}

// finiteNumber returns a float that JSON can hold. The three values JSON cannot
// hold become their own names, because a silent drop would hide the field.
func finiteNumber(f float64) any {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "+Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	}
	return f
}

// errorMessage returns the message of an error.
//
// A typed nil pointer satisfies error and panics on Error(), and a user type may
// panic for its own reasons, so the call runs under recover and the panic
// becomes a readable text.
func errorMessage(err error) (out string) {
	if err == nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			out = "[error: " + typeName(err) + " panicked]"
		}
	}()
	return err.Error()
}

// decodeJSON decodes JSON bytes into the owned tree. A number keeps its digits in
// a json.Number, so a large integer never loses precision.
func decodeJSON(raw []byte, depth int) any {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "[unencodable: json.RawMessage]"
	}
	return copyAt(v, depth+1)
}

// typeName names the Go type of a value for a fallback, without a package path
// that a reader cannot use.
func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	t := reflect.TypeOf(v)
	if t.Name() != "" {
		return t.Name()
	}
	return t.String()
}
