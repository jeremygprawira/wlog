// This file holds the OTel preset: the OTel log data model with the semantic
// conventions of v1.43.0, which the OTel Collector filelog receiver reads.
package preset

import (
	"net"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// otelLead is the top-level order the OTel preset prints first.
var otelLead = []string{"timestamp", "severity_text", "severity_number", "body", "event_name"}

// otelTopKeys names the top-level keys the preset writes itself, so a user key with
// one of these names moves aside.
var otelTopKeys = map[string]bool{
	"timestamp": true, "severity_text": true, "severity_number": true,
	"body": true, "event_name": true, "trace_id": true, "span_id": true,
	"resource": true, "attributes": true,
}

// OTel returns the preset that follows the OTel log data model and semantic conventions
// v1.43.0. The OTel Collector filelog receiver parses each printed line.
func OTel() wlog.OutputPreset { return otelPreset{} }

// otelPreset writes one OTel log record.
type otelPreset struct{}

// Name returns "otel".
func (otelPreset) Name() string { return "otel" }

// Lead returns the top-level keys the writer prints first.
func (otelPreset) Lead() []string { return otelLead }

// Apply returns the event as an OTel log record: the log record fields at the top, a flat
// resource object, and a flat attributes object. Every canonical key the table does not
// name keeps its canonical dotted path inside attributes.
func (otelPreset) Apply(event map[string]any) map[string]any {
	out := map[string]any{}
	target := &otelTarget{
		out:   out,
		attrs: map[string]any{},
		res:   map[string]any{},
		event: event,
	}
	otelErrorType(target)

	flat := map[string]any{}
	for key, value := range event {
		flatten(flat, key, value)
	}
	for path, value := range flat {
		switch {
		case strings.HasPrefix(path, "http.request_headers."):
			name := strings.ToLower(strings.TrimPrefix(path, "http.request_headers."))
			target.attrs["http.request.header."+name] = []any{value}
		case otelSkip[path]:
		case otelRules[path] != nil:
			otelRules[path](target, value)
		default:
			target.attrs[path] = value
		}
	}
	// A user key that names a top-level field the preset writes moves aside, so it never
	// replaces a field the collector reads.
	for key := range event {
		if otelTopKeys[key] && !wlog.IsReservedKey(key) {
			target.attrs["wlog.fields."+key] = event[key]
			delete(target.attrs, key)
		}
	}
	if len(target.res) > 0 {
		out["resource"] = target.res
	}
	if len(target.attrs) > 0 {
		out["attributes"] = target.attrs
	}
	return out
}

// otelTarget is the tree one OTel log record is written into.
type otelTarget struct {
	out   map[string]any // the log record fields
	attrs map[string]any // the flat attributes object
	res   map[string]any // the flat resource object
	event map[string]any // the canonical event, for a rule that reads a second field
}

// otelSkip names the canonical paths the preset consumes itself, so the walk does not
// also copy them into attributes.
var otelSkip = map[string]bool{
	"error.code": true, "error.kind": true,
	"rpc.service": true,
}

// otelRules maps one canonical path to the field it becomes. A rule that writes two
// fields, or that changes the value, lives here.
var otelRules = map[string]func(*otelTarget, any){
	"timestamp": func(t *otelTarget, v any) { t.out["timestamp"] = v },
	"summary":   func(t *otelTarget, v any) { t.out["body"] = v },
	"kind":      func(t *otelTarget, v any) { t.out["event_name"] = "wlog." + stringOf(v) },
	"level": func(t *otelTarget, v any) {
		text, number := otelSeverity(stringOf(v))
		t.out["severity_text"], t.out["severity_number"] = text, number
	},
	"trace.trace_id": func(t *otelTarget, v any) { t.out["trace_id"] = v },
	"trace.span_id":  func(t *otelTarget, v any) { t.out["span_id"] = v },

	"service.name":    func(t *otelTarget, v any) { t.res["service.name"] = v },
	"service.version": func(t *otelTarget, v any) { t.res["service.version"] = v },
	"service.env": func(t *otelTarget, v any) {
		t.res["deployment.environment.name"] = v
	},
	"service.instance": func(t *otelTarget, v any) { t.res["service.instance.id"] = v },

	"event_id":      func(t *otelTarget, v any) { t.attrs["log.record.uid"] = v },
	"error.message": func(t *otelTarget, v any) { t.attrs["exception.message"] = v },
	"error.type":    func(t *otelTarget, v any) { t.attrs["exception.type"] = v },
	"error.stack":   func(t *otelTarget, v any) { t.attrs["exception.stacktrace"] = v },

	"http.method": func(t *otelTarget, v any) { t.attrs["http.request.method"] = v },
	"http.path":   func(t *otelTarget, v any) { t.attrs["url.path"] = v },
	"http.status": func(t *otelTarget, v any) { t.attrs["http.response.status_code"] = v },
	"http.scheme": func(t *otelTarget, v any) { t.attrs["url.scheme"] = v },
	"http.host": func(t *otelTarget, v any) {
		host, port := splitHostPort(stringOf(v))
		t.attrs["server.address"] = host
		if port != "" {
			t.attrs["server.port"] = port
		}
	},
	"http.protocol": func(t *otelTarget, v any) {
		name, version := splitProtocol(stringOf(v))
		t.attrs["network.protocol.name"] = name
		if version != "" {
			t.attrs["network.protocol.version"] = version
		}
	},
	"http.client_ip": func(t *otelTarget, v any) { t.attrs["client.address"] = v },
	"http.user_agent": func(t *otelTarget, v any) {
		t.attrs["user_agent.original"] = v
	},
	"http.bytes_in":  func(t *otelTarget, v any) { t.attrs["http.request.body.size"] = v },
	"http.bytes_out": func(t *otelTarget, v any) { t.attrs["http.response.body.size"] = v },

	"rpc.system":      func(t *otelTarget, v any) { t.attrs["rpc.system.name"] = v },
	"rpc.status_code": func(t *otelTarget, v any) { t.attrs["rpc.response.status_code"] = v },
	"rpc.method": func(t *otelTarget, v any) {
		service, _ := pathValue(t.event, "rpc.service")
		t.attrs["rpc.method"] = stringOf(service) + "/" + stringOf(v)
	},

	"messaging.system": func(t *otelTarget, v any) { t.attrs["messaging.system"] = v },
	"messaging.destination": func(t *otelTarget, v any) {
		t.attrs["messaging.destination.name"] = v
	},
	"messaging.consumer_group": func(t *otelTarget, v any) {
		t.attrs["messaging.consumer.group.name"] = v
	},
	"messaging.message_id": func(t *otelTarget, v any) { t.attrs["messaging.message.id"] = v },
	"messaging.operation": func(t *otelTarget, v any) {
		operation := stringOf(v)
		if operation == "publish" {
			operation = "send"
		}
		t.attrs["messaging.operation.type"] = operation
	},
	"messaging.partition": func(t *otelTarget, v any) {
		t.attrs["messaging.destination.partition.id"] = numberString(v)
	},
	"messaging.batch_size": func(t *otelTarget, v any) {
		t.attrs["messaging.batch.message_count"] = v
	},
	"messaging.offset": func(t *otelTarget, v any) {
		system, _ := pathValue(t.event, "messaging.system")
		if stringOf(system) == "kafka" {
			t.attrs["messaging.kafka.offset"] = v
		}
	},

	"faas.name":          func(t *otelTarget, v any) { t.attrs["faas.name"] = v },
	"faas.version":       func(t *otelTarget, v any) { t.attrs["faas.version"] = v },
	"faas.trigger":       func(t *otelTarget, v any) { t.attrs["faas.trigger"] = v },
	"faas.invocation_id": func(t *otelTarget, v any) { t.attrs["faas.invocation_id"] = v },
	"faas.cold_start":    func(t *otelTarget, v any) { t.attrs["faas.coldstart"] = v },
	"faas.memory_mb": func(t *otelTarget, v any) {
		t.attrs["faas.max_memory"] = megabytesToBytes(v)
	},
	"faas.region": func(t *otelTarget, v any) { t.attrs["cloud.region"] = v },

	"user.id":    func(t *otelTarget, v any) { t.attrs["user.id"] = v },
	"user.email": func(t *otelTarget, v any) { t.attrs["user.email"] = v },
	"user.name":  func(t *otelTarget, v any) { t.attrs["user.name"] = v },

	"llm.provider":       func(t *otelTarget, v any) { t.attrs["gen_ai.provider.name"] = v },
	"llm.operation":      func(t *otelTarget, v any) { t.attrs["gen_ai.operation.name"] = v },
	"llm.request_model":  func(t *otelTarget, v any) { t.attrs["gen_ai.request.model"] = v },
	"llm.response_model": func(t *otelTarget, v any) { t.attrs["gen_ai.response.model"] = v },
	"llm.response_id":    func(t *otelTarget, v any) { t.attrs["gen_ai.response.id"] = v },
	"llm.finish_reasons": func(t *otelTarget, v any) {
		t.attrs["gen_ai.response.finish_reasons"] = v
	},
	"llm.input_tokens":  func(t *otelTarget, v any) { t.attrs["gen_ai.usage.input_tokens"] = v },
	"llm.output_tokens": func(t *otelTarget, v any) { t.attrs["gen_ai.usage.output_tokens"] = v },
	"llm.cache_read_input_tokens": func(t *otelTarget, v any) {
		t.attrs["gen_ai.usage.cache_read.input_tokens"] = v
	},
	"llm.cache_write_input_tokens": func(t *otelTarget, v any) {
		t.attrs["gen_ai.usage.cache_write.input_tokens"] = v
	},
	"llm.reasoning_tokens": func(t *otelTarget, v any) {
		t.attrs["gen_ai.usage.reasoning.output_tokens"] = v
	},
	// The message content is written only when a module's WithContent() filled it. The
	// upstream names are marked development, so this preset pins them and this comment
	// says so.
	"llm.input_messages":  func(t *otelTarget, v any) { t.attrs["gen_ai.input.messages"] = v },
	"llm.output_messages": func(t *otelTarget, v any) { t.attrs["gen_ai.output.messages"] = v },
}

// otelErrorType writes attributes.error.type from the first of error.code, error.kind,
// and error.type that carries a value. An event with an error and none of the three
// reports _OTHER, so a reader always finds a class.
func otelErrorType(t *otelTarget) {
	errInfo, ok := t.event["error"].(map[string]any)
	if !ok {
		return
	}
	for _, path := range []string{"code", "kind", "type"} {
		if value := stringOf(errInfo[path]); value != "" {
			t.attrs["error.type"] = value
			return
		}
	}
	t.attrs["error.type"] = "_OTHER"
}

// otelSeverity maps a wlog level to the OTel severity text and number.
func otelSeverity(level string) (string, int) {
	switch level {
	case "debug":
		return "debug", 5
	case "warn":
		return "warn", 13
	case "error":
		return "error", 17
	default:
		return "info", 9
	}
}

// splitHostPort splits a host into its address and its port. A host with no port keeps
// the whole value as the address, so an IPv6 literal without brackets stays whole.
func splitHostPort(host string) (string, string) {
	address, port, err := net.SplitHostPort(host)
	if err != nil {
		return host, ""
	}
	return address, port
}

// splitProtocol splits an HTTP protocol into its name and its version, so HTTP/1.1
// gives http and 1.1. A value with no slash keeps the whole value as the name.
func splitProtocol(protocol string) (string, string) {
	name, version, found := strings.Cut(protocol, "/")
	if !found {
		return strings.ToLower(protocol), ""
	}
	return strings.ToLower(name), version
}

// numberString renders a number as text, so a partition or a similar count becomes the
// string the OTel convention asks for. A string value is kept as it is.
func numberString(value any) string {
	switch number := value.(type) {
	case string:
		return number
	case int:
		return strconv.Itoa(number)
	case int64:
		return strconv.FormatInt(number, 10)
	case float64:
		return strconv.FormatFloat(number, 'f', -1, 64)
	default:
		return ""
	}
}

// megabytesToBytes converts a memory value in megabytes to bytes, which the OTel
// convention uses.
func megabytesToBytes(value any) any {
	switch number := value.(type) {
	case int:
		return int64(number) * 1_000_000
	case int64:
		return number * 1_000_000
	case float64:
		return int64(number * 1_000_000)
	default:
		return value
	}
}
