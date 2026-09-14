package wlogstd

import "strings"

// parseTraceparent extracts trace-id and parent-id (span-id) from a W3C traceparent
// header (https://www.w3.org/TR/trace-context/#traceparent-header): version "00",
// then a 32-hex-digit trace-id, a 16-hex-digit parent-id, and 2-hex-digit flags,
// dash-separated. A malformed header, or all-zero ids (reserved, meaning "none"),
// reports ok=false so the caller leaves trace.trace_id/span_id unset rather than
// logging garbage.
func parseTraceparent(header string) (traceID, spanID string, ok bool) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return "", "", false
	}
	version, tid, pid, flags := parts[0], parts[1], parts[2], parts[3]
	if version != "00" {
		return "", "", false
	}
	if !isHex(tid, 32) || tid == strings.Repeat("0", 32) {
		return "", "", false
	}
	if !isHex(pid, 16) || pid == strings.Repeat("0", 16) {
		return "", "", false
	}
	if !isHex(flags, 2) {
		return "", "", false
	}
	return tid, pid, true
}

func isHex(s string, wantLen int) bool {
	if len(s) != wantLen {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
