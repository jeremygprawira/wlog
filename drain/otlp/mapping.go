package otlp

import (
	"sort"
	"strconv"
	"time"

	"github.com/jeremygprawira/wlog/internal/version"
)

// The OTLP/HTTP JSON wire types. Field order is fixed, and attributes are sorted, so
// the same event always encodes to the same bytes.

type anyValue struct {
	StringValue *string      `json:"stringValue,omitempty"`
	BoolValue   *bool        `json:"boolValue,omitempty"`
	IntValue    *string      `json:"intValue,omitempty"`
	DoubleValue *float64     `json:"doubleValue,omitempty"`
	ArrayValue  *arrayValue  `json:"arrayValue,omitempty"`
	KvlistValue *kvlistValue `json:"kvlistValue,omitempty"`
}

type arrayValue struct {
	Values []anyValue `json:"values"`
}

type kvlistValue struct {
	Values []keyValue `json:"values"`
}

type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type logRecord struct {
	TimeUnixNano         string     `json:"timeUnixNano"`
	ObservedTimeUnixNano string     `json:"observedTimeUnixNano"`
	SeverityNumber       int        `json:"severityNumber"`
	SeverityText         string     `json:"severityText"`
	Body                 anyValue   `json:"body"`
	Attributes           []keyValue `json:"attributes,omitempty"`
	TraceID              string     `json:"traceId,omitempty"`
	SpanID               string     `json:"spanId,omitempty"`
}

type scope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type scopeLogs struct {
	Scope      scope       `json:"scope"`
	LogRecords []logRecord `json:"logRecords"`
}

type resource struct {
	Attributes []keyValue `json:"attributes,omitempty"`
}

type resourceLogs struct {
	Resource  resource    `json:"resource"`
	ScopeLogs []scopeLogs `json:"scopeLogs"`
}

type exportRequest struct {
	ResourceLogs []resourceLogs `json:"resourceLogs"`
}

// Severity numbers from the OTLP logs data model.
const (
	severityDebug = 5
	severityInfo  = 9
	severityWarn  = 13
	severityError = 17
)

// recordFor maps one event to one OTLP log record.
func recordFor(event map[string]any) logRecord {
	traceID, spanID := traceIDs(event)
	nanos := timeNanos(event)
	number, text := severityFor(event["level"])
	return logRecord{
		TimeUnixNano:         nanos,
		ObservedTimeUnixNano: nanos,
		SeverityNumber:       number,
		SeverityText:         text,
		Body:                 anyValue{StringValue: strPtr(bodyText(event))},
		Attributes:           attributesFor(event),
		TraceID:              traceID,
		SpanID:               spanID,
	}
}

// bodyText is the event's operation, or its message for a plain log line.
func bodyText(event map[string]any) string {
	if op, _ := event["operation"].(string); op != "" {
		return op
	}
	msg, _ := event["message"].(string)
	return msg
}

// severityFor maps a wlog level string to the OTLP severity number and text. An
// unknown or missing level is INFO.
func severityFor(level any) (int, string) {
	switch level {
	case "debug":
		return severityDebug, "DEBUG"
	case "warn":
		return severityWarn, "WARN"
	case "error":
		return severityError, "ERROR"
	default:
		return severityInfo, "INFO"
	}
}

// timeNanos reads the event's timestamp as Unix nanoseconds. A missing or bad
// timestamp uses now.
func timeNanos(event map[string]any) string {
	if text, ok := event["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return strconv.FormatInt(t.UnixNano(), 10)
		}
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// traceIDs reads trace.trace_id and trace.span_id, when the event has both.
func traceIDs(event map[string]any) (string, string) {
	trace, _ := event["trace"].(map[string]any)
	traceID, _ := trace["trace_id"].(string)
	spanID, _ := trace["span_id"].(string)
	return traceID, spanID
}

// attributesFor flattens every field that is not already part of the record into OTLP
// attributes. A nested map becomes dotted keys, and the result is sorted, so the
// payload is deterministic.
func attributesFor(event map[string]any) []keyValue {
	flat := map[string]any{}
	for key, value := range event {
		switch key {
		case "timestamp", "level", "operation", "message", "service":
			continue
		}
		flattenInto(key, value, flat)
	}

	keys := make([]string, 0, len(flat))
	for key := range flat {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	attributes := make([]keyValue, 0, len(keys))
	for _, key := range keys {
		value, ok := valueFor(flat[key])
		if !ok {
			continue
		}
		attributes = append(attributes, keyValue{Key: key, Value: value})
	}
	return attributes
}

// flattenInto writes value under path, recursing into maps with a dot. The record's
// own trace ids are skipped, because the record carries them as traceId and spanId.
func flattenInto(path string, value any, out map[string]any) {
	if path == "trace.trace_id" || path == "trace.span_id" {
		return
	}
	if nested, ok := value.(map[string]any); ok {
		for key, child := range nested {
			flattenInto(path+"."+key, child, out)
		}
		return
	}
	out[path] = value
}

// valueFor maps one Go value to an OTLP AnyValue. The bool result is false for nil and
// for any type the encoder does not support, so the caller skips it.
func valueFor(value any) (anyValue, bool) {
	switch v := value.(type) {
	case string:
		return anyValue{StringValue: strPtr(v)}, true
	case bool:
		return anyValue{BoolValue: &v}, true
	case int:
		text := strconv.Itoa(v)
		return anyValue{IntValue: &text}, true
	case int64:
		text := strconv.FormatInt(v, 10)
		return anyValue{IntValue: &text}, true
	case float64:
		return anyValue{DoubleValue: &v}, true
	case []any:
		values := make([]anyValue, 0, len(v))
		for _, item := range v {
			converted, ok := valueFor(item)
			if ok {
				values = append(values, converted)
			}
		}
		return anyValue{ArrayValue: &arrayValue{Values: values}}, true
	case map[string]any:
		// OTLP carries an object as a kvlistValue, so errors[], logs[], and any other
		// array of objects survives instead of arriving empty.
		values := make([]keyValue, 0, len(v))
		for key, item := range v {
			converted, ok := valueFor(item)
			if !ok {
				continue
			}
			values = append(values, keyValue{Key: key, Value: converted})
		}
		sort.Slice(values, func(i, j int) bool { return values[i].Key < values[j].Key })
		return anyValue{KvlistValue: &kvlistValue{Values: values}}, true
	default:
		return anyValue{}, false
	}
}

// resourceFor maps the event's service group to OTLP resource attributes.
func resourceFor(event map[string]any) resource {
	service, _ := event["service"].(map[string]any)
	if service == nil {
		return resource{}
	}
	var attributes []keyValue
	if name, ok := service["name"].(string); ok && name != "" {
		attributes = append(attributes, stringAttr("service.name", name))
	}
	if version, ok := service["version"].(string); ok && version != "" {
		attributes = append(attributes, stringAttr("service.version", version))
	}
	if env, ok := service["env"].(string); ok && env != "" {
		// The current semantic conventions name the environment attribute this way.
		attributes = append(attributes, stringAttr("deployment.environment.name", env))
	}
	return resource{Attributes: attributes}
}

func stringAttr(key, value string) keyValue {
	return keyValue{Key: key, Value: anyValue{StringValue: strPtr(value)}}
}

// canonicalResource makes one stable key for a resource, so events from the same
// service share one resourceLogs entry.
func canonicalResource(r resource) string {
	key := ""
	for _, attr := range r.Attributes {
		key += attr.Key + "="
		if attr.Value.StringValue != nil {
			key += *attr.Value.StringValue
		}
		key += "\x00"
	}
	return key
}

func strPtr(s string) *string { return &s }

// scopeFor names wlog as the instrumentation scope.
func scopeFor() scope { return scope{Name: "wlog", Version: version.Version} }
