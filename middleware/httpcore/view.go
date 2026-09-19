// This file holds the framework-neutral view of one request and one response. An adapter
// implements these interfaces for its own framework, so httpcore reads no framework type.
package httpcore

import (
	"net/http"
	"strings"
)

// Request is the framework-neutral view of one incoming request.
//
// Every method copies the value it returns, because a pooled framework context reuses its
// buffers after the handler returns.
type Request interface {
	Method() string
	Path() string
	Proto() string
	Header(name string) string // first value, any letter case
	EachHeader(fn func(name, value string))
	EachQuery(fn func(key, value string))
	EachCookie(fn func(name, value string))
	RemoteAddr() string   // the connection's ip:port
	ContentLength() int64 // -1 when unknown
}

// Response is the framework-neutral view of one response.
type Response interface {
	Status() int
	BytesWritten() int64 // -1 when unknown
	EachHeader(fn func(name, value string))
}

// SchemeReader is an optional Request method. An adapter whose connection may be
// encrypted reports "https" or "http", and core uses that value before the trusted-proxy
// rule. An adapter that does not implement it counts as plain http.
type SchemeReader interface {
	Scheme() string
}

// netHTTPRequest adapts a *net/http.Request.
type netHTTPRequest struct {
	r *http.Request
}

// Method returns the request method.
func (v netHTTPRequest) Method() string { return v.r.Method }

// Path returns the raw request path.
func (v netHTTPRequest) Path() string { return v.r.URL.Path }

// Proto returns the protocol, such as HTTP/1.1.
func (v netHTTPRequest) Proto() string { return v.r.Proto }

// Header returns the first value of one header. The Host header is a field of its own in
// net/http, so this view answers it from there.
func (v netHTTPRequest) Header(name string) string {
	if strings.EqualFold(name, "Host") {
		return v.r.Host
	}
	return v.r.Header.Get(name)
}

// Scheme returns https for a TLS connection, and http otherwise.
func (v netHTTPRequest) Scheme() string {
	if v.r.TLS != nil {
		return "https"
	}
	return "http"
}

// EachHeader visits every request header value.
func (v netHTTPRequest) EachHeader(fn func(name, value string)) {
	for name, values := range v.r.Header {
		for _, value := range values {
			fn(name, value)
		}
	}
}

// EachQuery visits every query value.
func (v netHTTPRequest) EachQuery(fn func(key, value string)) {
	for key, values := range v.r.URL.Query() {
		for _, value := range values {
			fn(key, value)
		}
	}
}

// EachCookie visits every request cookie.
func (v netHTTPRequest) EachCookie(fn func(name, value string)) {
	for _, cookie := range v.r.Cookies() {
		fn(cookie.Name, cookie.Value)
	}
}

// RemoteAddr returns the connection address, as ip:port.
func (v netHTTPRequest) RemoteAddr() string { return v.r.RemoteAddr }

// ContentLength returns the declared body size, or -1 when it is unknown.
func (v netHTTPRequest) ContentLength() int64 { return v.r.ContentLength }

// netHTTPResponse adapts the wrapper that observed the response.
type netHTTPResponse struct {
	w *statusWriter
}

// Status returns the status the handler wrote, or 200 when it wrote none.
func (v netHTTPResponse) Status() int { return v.w.status }

// BytesWritten returns the number of body bytes the handler wrote, or -1 when unknown.
func (v netHTTPResponse) BytesWritten() int64 {
	if !v.w.wrote {
		return -1
	}
	return v.w.bytes
}

// EachHeader visits every response header value.
func (v netHTTPResponse) EachHeader(fn func(name, value string)) {
	for name, values := range v.w.Header() {
		for _, value := range values {
			fn(name, value)
		}
	}
}
