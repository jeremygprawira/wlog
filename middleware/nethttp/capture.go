package wlogstd

import (
	"net"
	"net/http"
	"strings"
)

// clientIP prefers the first X-Forwarded-For entry (the original client, per the
// header's de-facto convention), falling back to RemoteAddr with its port stripped.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		first := strings.TrimSpace(strings.Split(fwd, ",")[0])
		if first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// captureHeaders converts http.Header to a flat map[string]any (first value per key),
// the shape wlog's redactor walks.
func captureHeaders(h http.Header) map[string]any {
	m := make(map[string]any, len(h))
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

func captureQuery(r *http.Request) map[string]any {
	q := r.URL.Query()
	m := make(map[string]any, len(q))
	for k, v := range q {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

func captureCookies(r *http.Request) map[string]any {
	cookies := r.Cookies()
	m := make(map[string]any, len(cookies))
	for _, c := range cookies {
		m[c.Name] = c.Value
	}
	return m
}
