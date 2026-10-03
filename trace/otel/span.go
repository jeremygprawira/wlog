// This file builds the span side of the plugin: the attributes, the status, and the one
// exception event. It reads the redacted event, so a masked value never reaches a span.
package wlogotel

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/preset"
)

// OnFinish copies the redacted event onto the active span. With no recording span it
// does nothing.
func (p *plugin) OnFinish(ctx context.Context, event wlog.Event) {
	if !p.cfg.spans {
		return
	}
	// A log line is its own event. It must not copy its pairs onto the request span.
	if kind, _ := event.Get("kind"); kind == "log" {
		return
	}
	span := oteltrace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	// A nested unit shares the outer span. Only the unit that claimed it may write.
	if !spanIsMine(ctx, span.SpanContext().SpanID()) {
		return
	}
	fields := eventFields(event)
	if fields == nil {
		return
	}
	attrs := spanAttributes(fields)
	if extra := spanErrorType(event); extra != "" {
		attrs = append(attrs, attribute.String("error.type", extra))
	}
	span.SetAttributes(attrs...)

	if outcome, _ := event.Get("outcome"); outcome == "error" {
		span.SetStatus(codes.Error, messageOf(event))
	}
	if p.cfg.exception {
		if _, ok := event.Get("error"); ok {
			span.AddEvent("exception", oteltrace.WithAttributes(exceptionAttributes(event)...))
		}
	}
}

// fieldView is the optional interface core's event view implements, so a Finisher that
// needs every key can read the whole redacted map.
type fieldView interface {
	Fields() map[string]any
}

// eventFields returns the whole event, or nil when the view does not offer it.
func eventFields(event wlog.Event) map[string]any {
	if fv, ok := event.(fieldView); ok {
		return fv.Fields()
	}
	return nil
}

// spanAttributes maps the event to OTel attributes through the otel output preset, so a
// span and a log record name the same field the same way. The four array groups that
// belong to another signal stay out.
func spanAttributes(event map[string]any) []attribute.KeyValue {
	record := preset.OTel().Apply(event)
	raw, _ := record["attributes"].(map[string]any)
	names := make([]string, 0, len(raw))
	for name := range raw {
		if skipSpanAttribute(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, rj := attributeRank(names[i]), attributeRank(names[j])
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	out := make([]attribute.KeyValue, 0, len(names))
	for _, name := range names {
		out = append(out, attributeOf(name, raw[name]))
	}
	return out
}

// mappedSpanAttribute names the attributes the otel preset writes for a reserved
// key. Rank 0 is this set plus error.type, so the span limit keeps them.
var mappedSpanAttribute = map[string]bool{
	"client.address": true, "cloud.region": true, "error.type": true,
	"exception.message": true, "exception.stacktrace": true, "exception.type": true,
	"faas.coldstart": true, "faas.invocation_id": true, "faas.max_memory": true,
	"faas.name": true, "faas.trigger": true, "faas.version": true,
	"gen_ai.input.messages": true, "gen_ai.operation.name": true,
	"gen_ai.output.messages": true, "gen_ai.provider.name": true,
	"gen_ai.request.model": true, "gen_ai.response.finish_reasons": true,
	"gen_ai.response.id": true, "gen_ai.response.model": true,
	"gen_ai.usage.cache_read.input_tokens":  true,
	"gen_ai.usage.cache_write.input_tokens": true,
	"gen_ai.usage.input_tokens":             true, "gen_ai.usage.output_tokens": true,
	"gen_ai.usage.reasoning.output_tokens": true,
	"http.request.body.size":               true, "http.request.method": true,
	"http.response.body.size": true, "http.response.status_code": true,
	"log.record.uid": true, "messaging.batch.message_count": true,
	"messaging.consumer.group.name": true, "messaging.destination.name": true,
	"messaging.destination.partition.id": true, "messaging.kafka.offset": true,
	"messaging.message.id": true, "messaging.operation.type": true,
	"messaging.system": true, "network.protocol.name": true,
	"network.protocol.version": true, "rpc.method": true,
	"rpc.response.status_code": true, "rpc.system.name": true,
	"server.address": true, "server.port": true, "url.path": true, "url.scheme": true,
	"user.email": true, "user.id": true, "user.name": true, "user_agent.original": true,
}

// attributeRank orders one attribute for the span limit. Rank 0 is a preset-mapped
// reserved key or error.type. Rank 1 is another reserved key. Rank 2 is a group.
// Rank 3 is a user key.
func attributeRank(name string) int {
	switch {
	case mappedSpanAttribute[name] || strings.HasPrefix(name, "http.request.header."):
		return 0
	case strings.HasPrefix(name, "wlog.fields."):
		return 3
	case reservedAttribute(name):
		return 1
	case strings.Contains(name, "."):
		return 2
	default:
		return 3
	}
}

// reservedAttribute reports whether name is a reserved key or a field of one.
func reservedAttribute(name string) bool {
	if wlog.IsReservedKey(name) {
		return true
	}
	head, _, ok := strings.Cut(name, ".")
	return ok && wlog.IsReservedKey(head)
}

// skipSpanAttribute reports whether an attribute belongs to the log record, not the
// span. The call stats stay, because they summarize the calls the span holds.
func skipSpanAttribute(name string) bool {
	switch name {
	case "logs", "errors", "audit", "calls":
		return true
	}
	return false
}

// attributeOf converts one event value to an OTel attribute. A slice of one scalar type
// becomes a typed slice, and any other slice becomes one JSON string.
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
	case []string:
		return attribute.StringSlice(name, v)
	case []any:
		return sliceAttribute(name, v)
	default:
		return attribute.String(name, jsonText(value))
	}
}

// sliceAttribute converts a slice of one scalar type to a typed slice. A mixed or
// nested slice becomes one JSON string, because OTel holds no mixed slice.
func sliceAttribute(name string, values []any) attribute.KeyValue {
	if len(values) == 0 {
		return attribute.StringSlice(name, nil)
	}
	switch values[0].(type) {
	case string:
		out := make([]string, len(values))
		for i, value := range values {
			s, ok := value.(string)
			if !ok {
				return attribute.String(name, jsonText(values))
			}
			out[i] = s
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

// messageOf returns the error message of an event, or an empty string.
func messageOf(event wlog.Event) string {
	value, _ := event.Get("error.message")
	text, _ := value.(string)
	return text
}

// spanErrorType names the error of a span: the error code, then its kind, then its Go
// type. A request that ended 500 or higher with no error uses the status as text.
func spanErrorType(event wlog.Event) string {
	for _, path := range []string{"error.code", "error.kind", "error.type"} {
		if value, ok := event.Get(path); ok {
			if text, ok := value.(string); ok && text != "" {
				return text
			}
		}
	}
	if _, hasError := event.Get("error"); hasError {
		return ""
	}
	if value, ok := event.Get("http.status"); ok {
		if code, ok := intValue(value); ok && code >= 500 {
			return strconv.FormatInt(code, 10)
		}
	}
	return ""
}

// exceptionAttributes builds the fields of the one exception event.
func exceptionAttributes(event wlog.Event) []attribute.KeyValue {
	out := []attribute.KeyValue{}
	for _, pair := range []struct{ path, name string }{
		{"error.type", "exception.type"},
		{"error.message", "exception.message"},
		{"error.stack", "exception.stacktrace"},
	} {
		if value, ok := event.Get(pair.path); ok {
			out = append(out, attributeOf(pair.name, value))
		}
	}
	return out
}
