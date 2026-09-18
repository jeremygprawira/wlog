// This file holds the two read-only fallback formats: B3 from the Zipkin family, and
// X-Ray from AWS. A message that carries no valid traceparent still names its trace
// through one of these, and a caller opts in with WithB3 or WithXRay.
package propagate

import "strings"

// b3Trace reads a trace id from the B3 single header, and from the multi headers when the
// single one is absent.
//
// The single header is b3: {trace-id}-{span-id}-{sampling-state}-{parent-span-id}, where
// the last two parts are optional. The multi headers are X-B3-TraceId, X-B3-SpanId, and
// X-B3-Sampled.
func b3Trace(c Carrier) (traceID, parentSpanID string, sampled bool, ok bool) {
	if value := c.Get("b3"); value != "" {
		parts := strings.Split(value, "-")
		if len(parts) >= 2 && isHex(parts[0], 32) && isHex(parts[1], 16) {
			sampled = len(parts) < 3 || parts[2] == "1" || parts[2] == "d"
			return parts[0], parts[1], sampled, true
		}
	}
	trace := c.Get("X-B3-TraceId")
	span := c.Get("X-B3-SpanId")
	if !isHex(trace, 32) || !isHex(span, 16) {
		return "", "", false, false
	}
	return trace, span, c.Get("X-B3-Sampled") == "1", true
}

// xrayTrace reads a trace id from the X-Amzn-Trace-Id header, and from the
// AWSTraceHeader that SQS and SNS add when it is absent.
//
// The header is a list of pairs: Root=1-5759e988-bd862e3fe1be46a994272793;Parent=... .
// The root carries a version digit, the epoch, and the id, so the trace id is the root
// without its first part and without its dashes.
func xrayTrace(c Carrier) (traceID, parentSpanID string, sampled bool, ok bool) {
	header := c.Get("X-Amzn-Trace-Id")
	if header == "" {
		header = c.Get("AWSTraceHeader")
	}
	if header == "" {
		return "", "", false, false
	}
	for _, pair := range strings.Split(header, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found {
			continue
		}
		switch strings.ToLower(key) {
		case "root":
			traceID = xrayID(value)
		case "parent":
			if isHex(value, 16) {
				parentSpanID = value
			}
		case "sampled":
			sampled = value == "1"
		}
	}
	if traceID == "" {
		return "", "", false, false
	}
	return traceID, parentSpanID, sampled, true
}

// xrayID turns an X-Ray root into a trace id. A root is 1-5759e988-bd862e3fe1be46a994272793,
// and the version digit and the dashes fall away.
func xrayID(root string) string {
	parts := strings.Split(root, "-")
	if len(parts) != 3 || parts[0] != "1" {
		return ""
	}
	id := parts[1] + parts[2]
	if !isHex(id, 32) {
		return ""
	}
	return id
}
