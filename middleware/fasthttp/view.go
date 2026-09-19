// This file holds the fasthttp views: the two types that translate one *fasthttp.RequestCtx
// into what http-core reads. Every method copies its value, because fasthttp reuses the
// underlying buffers after the handler returns.
package wlogfasthttp

import (
	"strings"

	"github.com/valyala/fasthttp"
)

// RequestView adapts one *fasthttp.RequestCtx to the http-core request interface. The
// Fiber adapters build on it, because a Fiber context carries the same fasthttp request.
type RequestView struct {
	Ctx *fasthttp.RequestCtx
}

// Method returns the request method.
func (v RequestView) Method() string { return clone(v.Ctx.Method()) }

// Path returns the decoded request path.
func (v RequestView) Path() string { return clone(v.Ctx.Path()) }

// Proto returns the protocol, such as HTTP/1.1.
func (v RequestView) Proto() string { return clone(v.Ctx.Request.Header.Protocol()) }

// Header returns the first value of one header. The Host header is a field of its own in
// fasthttp, so this view answers it from there.
func (v RequestView) Header(name string) string {
	if strings.EqualFold(name, "Host") {
		return clone(v.Ctx.Host())
	}
	return clone(v.Ctx.Request.Header.Peek(name))
}

// Scheme returns https for a TLS connection, and http otherwise.
func (v RequestView) Scheme() string {
	if v.Ctx.IsTLS() {
		return "https"
	}
	return "http"
}

// EachHeader visits every request header value. The Host header stays out, because the
// view answers it from its own field, as net/http does.
func (v RequestView) EachHeader(fn func(name, value string)) {
	for name, value := range v.Ctx.Request.Header.All() {
		if strings.EqualFold(string(name), fasthttp.HeaderHost) {
			continue
		}
		fn(clone(name), clone(value))
	}
}

// EachQuery visits every query value.
func (v RequestView) EachQuery(fn func(key, value string)) {
	for key, value := range v.Ctx.QueryArgs().All() {
		fn(clone(key), clone(value))
	}
}

// EachCookie visits every request cookie.
func (v RequestView) EachCookie(fn func(name, value string)) {
	for name, value := range v.Ctx.Request.Header.Cookies() {
		fn(clone(name), clone(value))
	}
}

// RemoteAddr returns the connection address, as ip:port.
func (v RequestView) RemoteAddr() string { return v.Ctx.RemoteAddr().String() }

// ContentLength returns the declared body size. A request that declares none reports the
// size of the body fasthttp read, so a chunked or an identity body reports its real size.
func (v RequestView) ContentLength() int64 {
	header := &v.Ctx.Request.Header
	if len(header.Peek(fasthttp.HeaderContentLength)) > 0 {
		if length := header.ContentLength(); length >= 0 {
			return int64(length)
		}
	}
	return int64(len(v.Ctx.PostBody()))
}

// ResponseView adapts one *fasthttp.RequestCtx to the http-core response interface.
type ResponseView struct {
	Ctx *fasthttp.RequestCtx
}

// Status returns the status of the response.
func (v ResponseView) Status() int { return v.Ctx.Response.StatusCode() }

// BytesWritten returns the number of body bytes in the response, and -1 for a body stream,
// whose size is unknown until the stream ends.
func (v ResponseView) BytesWritten() int64 {
	if v.Ctx.Response.IsBodyStream() {
		return -1
	}
	return int64(len(v.Ctx.Response.Body()))
}

// EachHeader visits every response header value. fasthttp answers a Content-Type for
// every response, and invents a default one when the handler set none. The view reads the
// header with that invention turned off for the moment of the read, then restores the
// state the app had, so the wire response never changes.
func (v ResponseView) EachHeader(fn func(name, value string)) {
	header := &v.Ctx.Response.Header
	inventedOff := len(header.ContentType()) == 0
	header.SetNoDefaultContentType(true)
	contentType := clone(header.Peek(fasthttp.HeaderContentType))
	header.SetNoDefaultContentType(inventedOff)

	for name, value := range header.All() {
		if strings.EqualFold(string(name), fasthttp.HeaderContentType) {
			continue
		}
		fn(clone(name), clone(value))
	}
	if contentType != "" {
		fn(fasthttp.HeaderContentType, contentType)
	}
}

// clone copies one value out of a fasthttp buffer, because fasthttp reuses that buffer
// after the handler returns.
func clone(value []byte) string { return strings.Clone(string(value)) }
