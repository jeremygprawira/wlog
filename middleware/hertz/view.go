// This file holds the hertz views: the two types that translate one *app.RequestContext
// into what http-core reads. Every method copies its value, because hertz recycles the
// context and its buffers after the request.
package wloghertz

import (
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

// RequestView adapts one *app.RequestContext to the http-core request interface.
type RequestView struct {
	Ctx *app.RequestContext
}

// Method returns the request method.
func (v RequestView) Method() string { return string(v.Ctx.Method()) }

// Path returns the decoded request path.
func (v RequestView) Path() string { return string(v.Ctx.Path()) }

// Proto returns the protocol, such as HTTP/1.1.
func (v RequestView) Proto() string { return v.Ctx.Request.Header.GetProtocol() }

// Header returns the first value of one header. The Host header is a field of its own in
// hertz, so this view answers it from there.
func (v RequestView) Header(name string) string {
	if strings.EqualFold(name, "Host") {
		return string(v.Ctx.Host())
	}
	return string(v.Ctx.Request.Header.Peek(name))
}

// EachHeader visits every request header value. The Host and Content-Length headers stay
// out, because net/http keeps both outside the header map, and the suite's golden events
// follow net/http.
func (v RequestView) EachHeader(fn func(name, value string)) {
	v.Ctx.Request.Header.VisitAll(func(name, value []byte) {
		if strings.EqualFold(string(name), "Host") ||
			strings.EqualFold(string(name), "Content-Length") {
			return
		}
		fn(strings.Clone(string(name)), strings.Clone(string(value)))
	})
}

// EachQuery visits every query value.
func (v RequestView) EachQuery(fn func(key, value string)) {
	v.Ctx.QueryArgs().VisitAll(func(key, value []byte) {
		fn(strings.Clone(string(key)), strings.Clone(string(value)))
	})
}

// EachCookie visits every request cookie.
func (v RequestView) EachCookie(fn func(name, value string)) {
	v.Ctx.Request.Header.VisitAllCookie(func(name, value []byte) {
		fn(strings.Clone(string(name)), strings.Clone(string(value)))
	})
}

// RemoteAddr returns the connection address, as ip:port.
func (v RequestView) RemoteAddr() string { return v.Ctx.RemoteAddr().String() }

// ContentLength returns the declared body size, or -1 when it is unknown.
func (v RequestView) ContentLength() int64 {
	return int64(v.Ctx.Request.Header.ContentLength())
}

// ResponseView adapts one *app.RequestContext to the http-core response interface.
type ResponseView struct {
	Ctx *app.RequestContext
}

// Status returns the status of the response, and 200 when the handler wrote none.
func (v ResponseView) Status() int {
	if code := v.Ctx.Response.StatusCode(); code != 0 {
		return code
	}
	return http.StatusOK
}

// BytesWritten returns the number of body bytes in the response, and -1 for a body
// stream, whose size is unknown until the stream ends.
func (v ResponseView) BytesWritten() int64 {
	if v.Ctx.Response.IsBodyStream() {
		return -1
	}
	return int64(len(v.Ctx.Response.Body()))
}

// EachHeader visits every response header value. hertz invents a Content-Type for every
// response, so the view turns that invention off for the moment of the read, then restores
// the state the app had. The wire response never changes.
func (v ResponseView) EachHeader(fn func(name, value string)) {
	header := &v.Ctx.Response.Header
	noDefault := header.NoDefaultContentType()
	header.SetNoDefaultContentType(true)
	header.VisitAll(func(name, value []byte) {
		fn(strings.Clone(string(name)), strings.Clone(string(value)))
	})
	header.SetNoDefaultContentType(noDefault)
}
