// This file holds the exchange: the shared HTTP pipeline that every adapter drives. One
// Core is built per middleware, and one Exchange tracks one request from Start to End.

// Package httpcore is the framework-neutral HTTP core that every wlog HTTP adapter
// drives. One Core owns capture, the operation name, the level, the route rules, trace
// context, and the emit point. An adapter passes only what its framework knows, at the
// moment it knows it.
package httpcore

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Core is the shared HTTP pipeline of one middleware. Build it once with New, and drive
// one Exchange per request.
type Core struct {
	log *wlog.Logger
	cfg config
}

// New builds the Core of one middleware. A nil Logger means wlog.Default.
func New(log *wlog.Logger, opts ...Option) *Core {
	if log == nil {
		log = wlog.Default()
	}
	cfg := newConfig(log, opts)
	return &Core{log: log, cfg: cfg}
}

// Skip reports whether this request starts no event at all. A skipped request still runs
// the handler, and it runs no plugin.
func (c *Core) Skip(r Request) bool {
	if c.cfg.skip != nil && c.cfg.skip(r) {
		return true
	}
	for _, pattern := range c.cfg.skipPaths {
		if globMatch(pattern, r.Path()) {
			return true
		}
	}
	return false
}

// Start opens one request event, or joins the event that this context already holds.
//
// It extracts the trace context and the request id through propagate, writes the request
// fields the policy allows, and returns the Exchange that ends the event. An event that
// is already open on ctx is reused, so a router mounted inside another wrapped router
// adds its fields to the outer event.
func (c *Core) Start(ctx context.Context, r Request) (context.Context, *Exchange) {
	operation := r.Method() + " unmatched"
	if wlog.HasEvent(ctx) {
		x := &Exchange{core: c, ctx: ctx, method: r.Method()}
		x.writeRequest(r)
		return ctx, x
	}
	ctx, end := wlog.Start(c.log.WithContext(ctx), operation)
	ctx = propagate.Extract(ctx, c.requestIDCarrier(r))
	x := &Exchange{core: c, ctx: ctx, end: end, owned: true, method: r.Method()}
	x.writeRequest(r)
	return ctx, x
}

// requestIDCarrier returns the carrier that propagate reads. An app that does not trust
// an incoming request id passes a carrier that hides the header, so core generates one.
func (c *Core) requestIDCarrier(r Request) propagate.Carrier {
	carrier := requestCarrier{r}
	if c.cfg.trustRequestID {
		return carrier
	}
	return untrustedRequestID{carrier}
}

// Exchange tracks one request from Start to End. It is not safe for concurrent use,
// because one request is one sequence of events.
type Exchange struct {
	core        *Core
	ctx         context.Context
	end         func()
	owned       bool // true when this Exchange started the event and must end it
	method      string
	route       string
	matched     bool
	operationID string
	ended       bool
}

// Route records the route template the router matched, and whether it matched one at all.
// An adapter calls it as soon as its framework knows the route, any time before End.
func (x *Exchange) Route(template string, matched bool) {
	x.route, x.matched = template, matched
}

// OperationID records the framework's own operation id, such as the huma operation id.
func (x *Exchange) OperationID(id string) { x.operationID = id }

// End writes the response fields, records the framework error, picks the level, and emits
// the event. A second End does nothing.
func (x *Exchange) End(resp Response, err error) {
	if x.ended {
		return
	}
	x.ended = true

	status := http.StatusOK
	written := int64(-1)
	if resp != nil {
		status = resp.Status()
		written = resp.BytesWritten()
	}
	x.finish(resp, status, written, err)
	if x.owned && x.end != nil {
		x.end()
	}
}

// writeRequest records the request fields that safe defaults capture, plus the fields the
// policy allows.
func (x *Exchange) writeRequest(r Request) {
	fields := []any{
		"method", r.Method(),
		"path", r.Path(),
		"protocol", r.Proto(),
		"client_ip", x.core.clientIP(r),
		"user_agent", r.Header("User-Agent"),
	}
	scheme, host := x.core.schemeHost(r)
	fields = append(fields, "scheme", scheme, "host", host)
	if length := r.ContentLength(); length >= 0 {
		fields = append(fields, "bytes_in", length)
	}
	wlog.SetGroup(x.ctx, "http", fields...)
	x.core.captureRequest(x.ctx, r)
}

// finish writes the response fields and the level of one finished request.
//
// The route is empty when the router matched nothing, and when the status is 404 or 405
// and the template ends in /*, which is the wildcard template a group reports for an
// unmatched path. An empty route makes the operation {METHOD} unmatched.
func (x *Exchange) finish(resp Response, status int, written int64, err error) {
	route := x.route
	if !x.matched || ((status == http.StatusNotFound || status == http.StatusMethodNotAllowed) &&
		strings.HasSuffix(route, "/*")) {
		route = ""
	}
	operation := x.method + " unmatched"
	if route != "" {
		operation = x.method + " " + route
	}
	wlog.Set(x.ctx, "operation", operation)

	fields := []any{"route", route, "status", status}
	if written >= 0 {
		fields = append(fields, "bytes_out", written)
	}
	wlog.SetGroup(x.ctx, "http", fields...)
	if x.operationID != "" {
		wlog.SetGroup(x.ctx, "http", "operation_id", x.operationID)
	}
	x.core.responseHeaders(x.ctx, x.core.routeConfig(x.method, route), resp)

	if err != nil {
		wlog.Error(x.ctx, err)
	}
	info, hasError := wlog.CurrentError(x.ctx)
	if level := levelFor(status, hasError, info.Status); level != "" {
		wlog.SetLevel(x.ctx, level)
	}
}

// levelFor returns the level the status and the error ask for, by the rule of SPEC.md.
//
// A recorded error gives error, and two cases give warn instead: the error carries a
// status from 400 to 499, or the response is a client error. With no error, the status
// class decides.
func levelFor(status int, hasError bool, errorStatus int) wlog.Level {
	class := work.ClassOf(work.KindRequest, strconv.Itoa(status))
	if hasError {
		if class == work.StatusClientError || (errorStatus >= 400 && errorStatus <= 499) {
			return wlog.LevelWarn
		}
		return wlog.LevelError
	}
	switch class {
	case work.StatusClientError:
		return wlog.LevelWarn
	case work.StatusServerError:
		return wlog.LevelError
	}
	return ""
}

// requestCarrier reads the incoming headers of one request through the view.
type requestCarrier struct {
	r Request
}

// Get returns the first value of one header.
func (c requestCarrier) Get(name string) string { return c.r.Header(name) }

// Set does nothing, because an incoming request is read-only.
func (requestCarrier) Set(string, string) {}

// untrustedRequestID hides the X-Request-ID header, so propagate generates an id instead
// of keeping a value the client chose.
type untrustedRequestID struct {
	requestCarrier
}

// Get returns an empty string for the request id header, and the real value otherwise.
func (c untrustedRequestID) Get(name string) string {
	if strings.EqualFold(name, "X-Request-ID") {
		return ""
	}
	return c.requestCarrier.Get(name)
}

// Keys returns the header names, which a read-only fallback format reads.
func (c requestCarrier) Keys() []string {
	keys := []string{}
	seen := map[string]bool{}
	c.r.EachHeader(func(name, value string) {
		if seen[name] {
			return
		}
		seen[name] = true
		keys = append(keys, name)
	})
	return keys
}
