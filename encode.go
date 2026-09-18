// This file writes one event as JSON in the reserved key order. A plain map marshals
// in key order by accident, which would make the output of one event depend on the
// Go runtime rather than on the schema, so the writer walks the key table itself.
package wlog

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
)

// reservedKeyOrder is the reserved key table of SPEC-core-v2, in output order. A key
// that is not listed here is a user key, and it sorts after the groups.
var reservedKeyOrder = []string{
	"timestamp", "level", "summary", "operation", "kind", "outcome", "duration_ms",
	"message", "error", "event_id", "service", "trace",
	"http", "rpc", "messaging", "job", "cli", "faas",
	"user", "geo", "client", "host", "deploy", "llm",
	"audit", "errors", "logs", "calls", "call_stats", "feature_flags", "wlog",
}

// reservedTail holds the reserved keys that follow the user keys in the table, so the
// writer knows where the user keys belong.
var reservedTail = []string{"audit", "errors", "logs", "calls", "call_stats", "feature_flags", "wlog"}

// reservedTailRank maps a tail key to its place.
var reservedTailRank = func() map[string]int {
	rank := make(map[string]int, len(reservedTail))
	for i, key := range reservedTail {
		rank[key] = i
	}
	return rank
}()

// nestedKeyOrder names the field order of the objects core builds itself. The specs
// pin each group order, and a reader compares two events field by field, so the writer
// follows the tables rather than the alphabet.
var nestedKeyOrder = map[string][]string{
	"service":   {"name", "version", "env", "instance"},
	"http":      {"method", "route", "path", "status", "protocol", "scheme", "host", "bytes_in", "bytes_out", "client_ip", "user_agent"},
	"trace":     {"trace_id", "span_id", "parent_span_id", "request_id", "parent_event_id", "parent_operation"},
	"wlog":      {"schema_version", "redact_fingerprint", "sample_rate", "dropped_fields", "dropped_logs", "dropped_errors", "dropped_calls", "dropped_audit", "late_writes", "unknown_keys", "truncated"},
	"rpc":       {"system", "service", "method", "status_code", "protocol", "peer", "stream", "messages_sent", "messages_received", "request_size", "response_size"},
	"messaging": {"system", "operation", "destination", "consumer_group", "message_id", "partition", "offset", "delivery_count", "redelivered", "result", "lag_ms", "batch_size", "body_size"},
	"job":       {"system", "name", "id", "queue", "schedule", "attempt", "max_attempts", "result", "scheduled_at", "lag_ms"},
	"cli":       {"name", "path", "args_count", "flags", "exit_code"},
	"faas":      {"system", "name", "version", "trigger", "invocation_id", "cold_start", "remaining_ms", "memory_mb", "region", "batch_size", "batch_failures"},
}

// rankOf returns the position of key inside order, or the length of order when the key
// is not listed, so an unknown field sorts last rather than first.
func rankOf(order []string, key string) int {
	for i, name := range order {
		if name == key {
			return i
		}
	}
	return len(order)
}

// reservedRank maps a reserved key to its place in the table.
var reservedRank = func() map[string]int {
	rank := make(map[string]int, len(reservedKeyOrder))
	for i, key := range reservedKeyOrder {
		rank[key] = i
	}
	return rank
}()

// orderedKeys returns every key of the event: the reserved keys in table order, then
// the user keys sorted by name, then the arrays and the wlog object that close the
// table.
func orderedKeys(out map[string]any) []string {
	head := make([]string, 0, len(out))
	tail := make([]string, 0, len(reservedTail))
	user := make([]string, 0, len(out))
	for key := range out {
		switch {
		case reservedTailRank[key] > 0 || key == reservedTail[0]:
			tail = append(tail, key)
		case reservedRank[key] > 0 || key == reservedKeyOrder[0]:
			head = append(head, key)
		default:
			user = append(user, key)
		}
	}
	sort.Slice(head, func(i, j int) bool { return reservedRank[head[i]] < reservedRank[head[j]] })
	sort.Slice(tail, func(i, j int) bool { return reservedTailRank[tail[i]] < reservedTailRank[tail[j]] })
	sort.Strings(user)
	keys := append(head, user...)
	return append(keys, tail...)
}

// orderedNested returns the keys of one object in the order its table names, with the
// unlisted keys sorted by name at the end.
func orderedNested(order []string, value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if left, right := rankOf(order, keys[i]), rankOf(order, keys[j]); left != right {
			return left < right
		}
		return keys[i] < keys[j]
	})
	return keys
}

// encodeEvent returns out as one JSON line in the reserved key order of the default
// shape. HTML escaping is off, so a URL or a query string reads as it was written.
//
// A key whose value is an empty string, a nil, or an empty object or array is left
// out, because the schema gives an absent value and an empty one the same meaning, and
// the shorter line costs less to store.
func encodeEvent(out map[string]any) ([]byte, error) {
	return encodeOrdered(out, orderedKeys(out), true)
}

// encodePreset returns out as one JSON line in the order a preset asks for: the lead
// keys first and in that order, then every other key sorted by name. Nested keys sort by
// name too, because a preset writes its own objects.
func encodePreset(out map[string]any, lead []string) ([]byte, error) {
	return encodeOrdered(out, leadKeys(out, lead), false)
}

// leadKeys returns the keys of out in preset order: the lead keys present in out, then
// the rest sorted by name.
func leadKeys(out map[string]any, lead []string) []string {
	keys := make([]string, 0, len(out))
	taken := make(map[string]bool, len(lead))
	for _, key := range lead {
		if _, present := out[key]; present && !taken[key] {
			taken[key] = true
			keys = append(keys, key)
		}
	}
	rest := make([]string, 0, len(out))
	for key := range out {
		if !taken[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// encodeOrdered writes out as one JSON object with keys in the given order. A key whose
// value is empty is left out. When fixedNested is true, an object whose field order a
// table fixes keeps that order.
func encodeOrdered(out map[string]any, keys []string, fixedNested bool) ([]byte, error) {
	buf := bytes.Buffer{}
	buf.WriteByte('{')
	var enc json.Encoder
	first := true
	for _, key := range keys {
		value := out[key]
		if isEmptyValue(value) {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(quote(key))
		buf.WriteByte(':')

		var body bytes.Buffer
		enc = *json.NewEncoder(&body)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(orderedValue(key, value, fixedNested)); err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimRight(body.Bytes(), "\n"))
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// orderedValue returns value unchanged, except for an object whose field order a table
// fixes, which becomes an orderedObject.
func orderedValue(key string, value any, fixedNested bool) any {
	object, ok := value.(map[string]any)
	if !ok || !fixedNested {
		return value
	}
	order, known := nestedKeyOrder[key]
	if !known {
		return value
	}
	return orderedObject{order: order, value: object}
}

// orderedObject is an object that renders its keys in a fixed order, so a nested group
// follows its table instead of the map.
type orderedObject struct {
	order []string
	value map[string]any
}

// MarshalJSON renders the object in its table order, and leaves out an empty value.
func (o orderedObject) MarshalJSON() ([]byte, error) {
	buf := bytes.Buffer{}
	buf.WriteByte('{')
	first := true
	for _, key := range orderedNested(o.order, o.value) {
		value := o.value[key]
		if isEmptyValue(value) {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(quote(key))
		buf.WriteByte(':')
		body, err := json.Marshal(orderedValue(key, value, true))
		if err != nil {
			return nil, err
		}
		buf.Write(body)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// quote renders one key as a JSON string. A key that the encoder cannot render falls
// back to an empty string rather than failing the whole event.
func quote(key string) string {
	body, err := json.Marshal(key)
	if err != nil {
		return `""`
	}
	return string(body)
}

// isEmptyValue reports whether a value carries nothing, which the schema treats as
// absent. A zero number counts as a value, because zero is a real answer for a status
// or a count, and the caller leaves out a counter it does not want written.
func isEmptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	}
	// A typed empty slice, such as a nil []string, carries nothing either.
	switch reflect.ValueOf(value).Kind() {
	case reflect.Slice, reflect.Map, reflect.Array:
		return reflect.ValueOf(value).Len() == 0
	}
	return false
}

// wlogObject builds the nested wlog object: the schema version, the redaction
// fingerprint, and the counters of what core had to drop. A zero counter is left out,
// so an event that lost nothing carries no noise.
func wlogObject(fingerprint string, counters map[string]any) map[string]any {
	out := map[string]any{"schema_version": schemaVersion}
	if fingerprint != "" {
		out["redact_fingerprint"] = fingerprint
	}
	for key, value := range counters {
		if isEmptyValue(value) || isZeroCounter(value) {
			continue
		}
		out[key] = value
	}
	return out
}

// isZeroCounter reports whether a counter value is the number zero, which the wlog
// object leaves out.
func isZeroCounter(value any) bool {
	switch v := value.(type) {
	case int:
		return v == 0
	case int64:
		return v == 0
	case float64:
		return v == 0
	}
	return false
}
