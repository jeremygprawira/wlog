// This file holds the ECS preset: the Elastic Common Schema 9.5.0 field names with the
// ecs-logging line rules.
package preset

import (
	"net/netip"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// ecsVersion is the ECS release the field names follow.
const ecsVersion = "9.5.0"

// ecsLead is the top-level order the ECS preset prints first. These four are literal
// dotted keys, and every other key is a nested object.
var ecsLead = []string{"@timestamp", "log.level", "message", "ecs.version"}

// ECS returns the preset that follows ECS 9.5.0 and the ecs-logging line rules. A user
// key moves under wlog.fields, so it never replaces a field ECS reads.
func ECS() wlog.OutputPreset { return ecsPreset{} }

// ecsPreset writes one ECS log line.
type ecsPreset struct{}

// Name returns "ecs".
func (ecsPreset) Name() string { return "ecs" }

// Lead returns the top-level keys the writer prints first.
func (ecsPreset) Lead() []string { return ecsLead }

// Apply returns the event with the ECS field names. A canonical key the table does not
// name keeps its canonical path under wlog, and a user key goes under wlog.fields.
func (ecsPreset) Apply(event map[string]any) map[string]any {
	out := map[string]any{"ecs.version": ecsVersion}
	flat := map[string]any{}
	for key, value := range event {
		if !wlog.IsReservedKey(key) {
			continue
		}
		flatten(flat, key, value)
	}
	for path, value := range flat {
		if rule, ok := ecsRules[path]; ok {
			rule(out, value)
			continue
		}
		setPath(out, ecsWlogPath(path), value)
	}

	fields := map[string]any{}
	for key, value := range event {
		if !wlog.IsReservedKey(key) {
			fields[key] = value
		}
	}
	if len(fields) > 0 {
		setPath(out, "wlog.fields", fields)
	}
	return out
}

// ecsRules maps one canonical path to the ECS field it becomes. A rule that writes two
// fields, or that changes the value, lives here.
var ecsRules = map[string]func(map[string]any, any){
	"timestamp": func(out map[string]any, v any) { out["@timestamp"] = v },
	"level":     func(out map[string]any, v any) { out["log.level"] = v },
	"summary":   func(out map[string]any, v any) { out["message"] = v },
	"operation": func(out map[string]any, v any) { setPath(out, "event.action", v) },
	"outcome": func(out map[string]any, v any) {
		outcome := stringOf(v)
		if outcome == "error" {
			outcome = "failure"
		}
		setPath(out, "event.outcome", outcome)
	},
	"duration_ms": func(out map[string]any, v any) { setPath(out, "event.duration", nanoseconds(v)) },
	"event_id":    ecsRename("event.id"),

	"error.message": ecsRename("error.message"),
	"error.type":    ecsRename("error.type"),
	"error.code":    ecsRename("error.code"),
	"error.stack":   ecsRename("error.stack_trace"),

	"service.name":     ecsRename("service.name"),
	"service.version":  ecsRename("service.version"),
	"service.env":      ecsRename("service.environment"),
	"service.instance": ecsRename("service.node.name"),

	"trace.trace_id":   ecsRename("trace.id"),
	"trace.span_id":    ecsRename("span.id"),
	"trace.request_id": ecsRename("http.request.id"),

	"http.method":    ecsRename("http.request.method"),
	"http.status":    ecsRename("http.response.status_code"),
	"http.path":      ecsRename("url.path"),
	"http.scheme":    ecsRename("url.scheme"),
	"http.bytes_in":  ecsRename("http.request.body.bytes"),
	"http.bytes_out": ecsRename("http.response.body.bytes"),
	"http.user_agent": func(out map[string]any, v any) {
		setPath(out, "user_agent.original", v)
	},
	"http.host": func(out map[string]any, v any) {
		host, port := splitHostPort(stringOf(v))
		setPath(out, "url.domain", host)
		if port != "" {
			setPath(out, "url.port", port)
		}
	},
	"http.protocol": func(out map[string]any, v any) {
		_, version := splitProtocol(stringOf(v))
		if version != "" {
			setPath(out, "http.version", version)
		}
	},
	"http.client_ip": func(out map[string]any, v any) {
		ip := stringOf(v)
		if _, err := netip.ParseAddr(ip); err == nil {
			setPath(out, "client.ip", ip)
			return
		}
		setPath(out, "client.address", ip)
	},

	"user.id":    ecsRename("user.id"),
	"user.email": ecsRename("user.email"),
	"user.name":  ecsRename("user.name"),
}

// ecsRename returns a rule that writes the value at one ECS path.
func ecsRename(to string) func(map[string]any, any) {
	return func(out map[string]any, v any) { setPath(out, to, v) }
}

// ecsWlogPath returns the place of a canonical key the table does not name. A key of the
// canonical wlog object already sits under wlog, so it stays where it is.
func ecsWlogPath(path string) string {
	if path == "wlog" || strings.HasPrefix(path, "wlog.") {
		return path
	}
	return "wlog." + path
}

// nanoseconds renders a millisecond duration as whole nanoseconds, which ECS reads.
func nanoseconds(value any) any {
	switch number := value.(type) {
	case int:
		return int64(number) * 1_000_000
	case int64:
		return number * 1_000_000
	case float64:
		return int64(number * 1_000_000)
	default:
		return int64(0)
	}
}
