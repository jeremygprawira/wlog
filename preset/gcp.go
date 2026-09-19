// This file holds the GCP preset: the Cloud Logging special JSON fields that Cloud Run,
// GKE, and Cloud Run functions read from stdout.
package preset

import (
	"os"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// gcpErrorType is the @type that puts an error event in Error Reporting.
const gcpErrorType = "type.googleapis.com/google.devtools.clouderrorreporting.v1beta1.ReportedErrorEvent"

// gcpLead is the top-level order the GCP preset prints first.
var gcpLead = []string{"time", "severity", "message"}

// gcpSkip names the canonical paths the preset writes itself, so the payload does not
// also hold them.
var gcpSkip = map[string]bool{
	"timestamp": true, "level": true, "summary": true,
	"trace.trace_id": true, "trace.span_id": true, "trace.request_id": true,
	"service.env": true, "kind": true, "outcome": true,
	"http.method": true, "http.status": true, "http.user_agent": true,
	"http.client_ip": true, "http.protocol": true, "http.scheme": true,
	"http.host": true, "http.path": true, "http.bytes_in": true, "http.bytes_out": true,
	"http.request_headers.referer": true, "error.stack": true,
}

// gcpTaken names the top-level keys GCP or GKE reserves. A key of the event with one of
// these names moves under wlog.fields, so it never replaces a reserved field.
var gcpTaken = map[string]bool{
	"time": true, "severity": true, "message": true, "stream": true,
	"httpRequest": true, "jsonPayload": true, "stack_trace": true, "serviceContext": true,
}

// GCPOption configures a GCP preset.
type GCPOption func(*gcpPreset)

// GCPProject sets the project of the trace resource name. Unset, it reads
// GOOGLE_CLOUD_PROJECT once, here.
func GCPProject(id string) GCPOption { return func(p *gcpPreset) { p.project = id } }

// GCP returns the preset that follows the Cloud Logging special JSON fields. Cloud Run,
// GKE, and Cloud Run functions read these fields from stdout.
func GCP(opts ...GCPOption) wlog.OutputPreset {
	p := gcpPreset{project: os.Getenv("GOOGLE_CLOUD_PROJECT")}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// gcpPreset writes one Cloud Logging entry.
type gcpPreset struct {
	project string
}

// Name returns "gcp".
func (gcpPreset) Name() string { return "gcp" }

// Lead returns the top-level keys the writer prints first.
func (gcpPreset) Lead() []string { return gcpLead }

// Apply returns the event as a Cloud Logging entry. A canonical key the table does not
// name keeps its canonical path in jsonPayload.
func (p gcpPreset) Apply(event map[string]any) map[string]any {
	out := map[string]any{}
	payload := map[string]any{}
	if value, ok := event["timestamp"]; ok {
		out["time"] = value
	}
	if value, ok := event["level"]; ok {
		out["severity"] = gcpSeverity(stringOf(value))
	}
	if value, ok := event["summary"]; ok {
		out["message"] = value
	}
	if value, ok := pathValue(event, "trace.trace_id"); ok {
		out["logging.googleapis.com/trace"] = gcpTrace(p.project, stringOf(value))
	}
	if value, ok := pathValue(event, "trace.span_id"); ok {
		out["logging.googleapis.com/spanId"] = value
	}
	p.labels(out, event)
	p.httpRequest(out, event)
	if value, ok := pathValue(event, "error.stack"); ok {
		out["stack_trace"] = value
	}
	if gcpIsError(event) {
		out["@type"] = gcpErrorType
		context := map[string]any{}
		if value, ok := pathValue(event, "service.name"); ok {
			context["service"] = value
		}
		if value, ok := pathValue(event, "service.version"); ok {
			context["version"] = value
		}
		if len(context) > 0 {
			out["serviceContext"] = context
		}
	}

	skip := gcpSkipFor(event)
	flat := map[string]any{}
	for key, value := range event {
		flatten(flat, key, value)
	}
	for path, value := range flat {
		if skip[path] {
			continue
		}
		setPath(payload, path, value)
	}
	for key, value := range event {
		if wlog.IsReservedKey(key) || !gcpTaken[key] {
			continue
		}
		setPath(out, "wlog.fields."+key, value)
		deletePath(payload, key)
	}
	if len(payload) > 0 {
		out["jsonPayload"] = payload
	}
	return out
}

// labels writes the Cloud Logging labels: the environment, the request id, the kind, and
// the outcome, each as text.
func (gcpPreset) labels(out map[string]any, event map[string]any) {
	labels := map[string]any{}
	for _, row := range []struct{ path, key string }{
		{"service.env", "env"}, {"trace.request_id", "request_id"},
		{"kind", "kind"}, {"outcome", "outcome"},
	} {
		if value, ok := pathValue(event, row.path); ok {
			labels[row.key] = textOf(value)
		}
	}
	if len(labels) > 0 {
		out["logging.googleapis.com/labels"] = labels
	}
}

// httpRequest writes the httpRequest object of a request.
func (gcpPreset) httpRequest(out map[string]any, event map[string]any) {
	request := map[string]any{}
	for _, row := range []struct{ path, key string }{
		{"http.method", "requestMethod"}, {"http.status", "status"},
		{"http.user_agent", "userAgent"}, {"http.client_ip", "remoteIp"},
		{"http.protocol", "protocol"},
	} {
		if value, ok := pathValue(event, row.path); ok {
			request[row.key] = value
		}
	}
	scheme, _ := pathValue(event, "http.scheme")
	host, _ := pathValue(event, "http.host")
	path, _ := pathValue(event, "http.path")
	if stringOf(host) != "" {
		request["requestUrl"] = stringOf(scheme) + "://" + stringOf(host) + stringOf(path)
	} else if stringOf(path) != "" {
		request["requestUrl"] = stringOf(path)
	}
	if value, ok := pathValue(event, "http.bytes_in"); ok {
		request["requestSize"] = numberString(value)
	}
	if value, ok := pathValue(event, "http.bytes_out"); ok {
		request["responseSize"] = numberString(value)
	}
	if kind, _ := event["kind"].(string); kind == "request" {
		if value, ok := pathValue(event, "duration_ms"); ok {
			request["latency"] = gcpLatency(value)
		}
	}
	if value, ok := pathValue(event, "http.request_headers.referer"); ok {
		request["referer"] = value
	}
	if len(request) > 0 {
		out["httpRequest"] = request
	}
}

// gcpSkipFor returns the canonical paths the preset writes itself for this event. The
// service identity and the duration move only in the cases the table names.
func gcpSkipFor(event map[string]any) map[string]bool {
	skip := make(map[string]bool, len(gcpSkip)+3)
	for path := range gcpSkip {
		skip[path] = true
	}
	if kind, _ := event["kind"].(string); kind == "request" {
		skip["duration_ms"] = true
	}
	if gcpIsError(event) {
		skip["service.name"] = true
		skip["service.version"] = true
	}
	return skip
}

// gcpIsError reports whether an event carries an error at level error, which is the case
// that carries serviceContext and @type.
func gcpIsError(event map[string]any) bool {
	if level, _ := event["level"].(string); level != "error" {
		return false
	}
	_, ok := event["error"].(map[string]any)
	return ok
}

// gcpSeverity maps a wlog level to the Cloud Logging severity text.
func gcpSeverity(level string) string {
	switch level {
	case "debug":
		return "DEBUG"
	case "warn":
		return "WARNING"
	case "error":
		return "ERROR"
	default:
		return "INFO"
	}
}

// gcpTrace renders the trace resource name. A project makes the full name, and no project
// keeps the bare id.
func gcpTrace(project, id string) string {
	if project == "" {
		return id
	}
	return "projects/" + project + "/traces/" + id
}

// gcpLatency renders a millisecond duration as seconds with an s, which httpRequest
// reads. Google counts up to nine decimals, so the text keeps nine and drops the rest.
func gcpLatency(value any) string {
	text := strconv.FormatFloat(floatValue(value)/1000, 'f', 9, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimSuffix(text, ".")
	return text + "s"
}
