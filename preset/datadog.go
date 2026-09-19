// This file holds the Datadog preset: the reserved and standard log attributes of the
// Datadog Agent, which reads nested JSON.
package preset

import "github.com/jeremygprawira/wlog"

// datadogLead is the top-level order the Datadog preset prints first.
var datadogLead = []string{"timestamp", "status", "message", "service"}

// datadogTaken names the top-level keys the preset writes itself. A key of the event with
// one of these names moves under wlog.fields, so it never replaces a Datadog field.
var datadogTaken = map[string]bool{
	"message": true, "status": true, "service": true, "dd": true,
	"trace_id": true, "span_id": true, "evt": true, "duration": true,
	"network": true, "usr": true, "logger": true,
}

// Datadog returns the preset that follows Datadog's reserved and standard log
// attributes. It never writes host, because a JSON host removes the host tags that the
// Agent adds.
func Datadog() wlog.OutputPreset { return datadogPreset{} }

// datadogPreset writes one Datadog log line.
type datadogPreset struct{}

// Name returns "datadog".
func (datadogPreset) Name() string { return "datadog" }

// Lead returns the top-level keys the writer prints first.
func (datadogPreset) Lead() []string { return datadogLead }

// Apply returns the event with the Datadog attribute names. A canonical key the table
// does not name keeps its canonical path, and a key that would replace a Datadog field
// moves under wlog.fields.
func (datadogPreset) Apply(event map[string]any) map[string]any {
	out := event
	for key, value := range out {
		if wlog.IsReservedKey(key) && key != "message" {
			continue
		}
		if !datadogTaken[key] {
			continue
		}
		setPath(out, "wlog.fields."+key, value)
		delete(out, key)
	}

	datadogService(out)
	movePath(out, "level", "status")
	movePath(out, "summary", "message")
	movePath(out, "trace.trace_id", "trace_id")
	movePath(out, "trace.span_id", "span_id")
	movePath(out, "trace.request_id", "http.request_id")
	movePath(out, "operation", "evt.name")
	datadogOutcome(out)
	datadogDuration(out)
	datadogError(out)
	movePath(out, "http.status", "http.status_code")
	movePath(out, "http.user_agent", "http.useragent")
	datadogHTTP(out)
	movePath(out, "http.client_ip", "network.client.ip")
	movePath(out, "http.bytes_in", "network.bytes_read")
	movePath(out, "http.bytes_out", "network.bytes_written")
	movePath(out, "user.id", "usr.id")
	movePath(out, "user.email", "usr.email")
	movePath(out, "user.name", "usr.name")
	setPath(out, "logger.name", "wlog")
	return out
}

// datadogService moves the service name to the top-level service field, and the other
// service fields to dd. A field the table does not name moves under wlog, so the string
// service field never hides it.
func datadogService(out map[string]any) {
	movePath(out, "service.env", "dd.env")
	movePath(out, "service.version", "dd.version")
	service, _ := out["service"].(map[string]any)
	name := service["name"]
	for key, value := range service {
		if key == "name" {
			continue
		}
		setPath(out, "wlog.service."+key, value)
	}
	delete(out, "service")
	if name != nil {
		out["service"] = name
	}
}

// datadogOutcome moves outcome to evt.outcome, and renders error as failure.
func datadogOutcome(out map[string]any) {
	value, ok := pathValue(out, "outcome")
	if !ok {
		return
	}
	outcome := stringOf(value)
	if outcome == "error" {
		outcome = "failure"
	}
	deletePath(out, "outcome")
	setPath(out, "evt.outcome", outcome)
}

// datadogDuration moves duration_ms to duration, in whole nanoseconds.
func datadogDuration(out map[string]any) {
	value, ok := pathValue(out, "duration_ms")
	if !ok {
		return
	}
	deletePath(out, "duration_ms")
	out["duration"] = nanoseconds(value)
}

// datadogError moves the first of error.code, error.kind, and error.type to error.kind,
// which the Datadog error tracker reads.
func datadogError(out map[string]any) {
	errInfo, ok := out["error"].(map[string]any)
	if !ok {
		return
	}
	kind := ""
	for _, key := range []string{"code", "kind", "type"} {
		if value := stringOf(errInfo[key]); value != "" {
			kind = value
			break
		}
	}
	delete(errInfo, "code")
	delete(errInfo, "kind")
	delete(errInfo, "type")
	if kind != "" {
		errInfo["kind"] = kind
	}
	if len(errInfo) == 0 {
		delete(out, "error")
	}
}

// datadogHTTP moves http.path to http.url_details.path and builds http.url from the
// scheme, the host, and the path. A host that is absent leaves http.url out.
func datadogHTTP(out map[string]any) {
	scheme, _ := pathValue(out, "http.scheme")
	host, _ := pathValue(out, "http.host")
	path, _ := pathValue(out, "http.path")
	if stringOf(host) != "" {
		setPath(out, "http.url", stringOf(scheme)+"://"+stringOf(host)+stringOf(path))
	}
	movePath(out, "http.path", "http.url_details.path")
	deletePath(out, "http.scheme")
	deletePath(out, "http.host")
}
