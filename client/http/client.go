// Package wlogclient records every outbound HTTP call as one call record on the open
// event, and it sends the trace headers of the caller.
//
// Read top to bottom: Transport wraps one http.RoundTripper. It starts a call before the
// round trip, adds traceparent and X-Request-ID to the request, and ends the call when the
// response headers arrive, so a large or a streamed body is never held. Resty and every
// http.Client user reach it through the transport of the client.
package wlogclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Transport wraps next, so every request becomes one call record on the event of the
// context. A nil next means http.DefaultTransport.
func Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &transport{next: next}
}

// transport records one call around the wrapped round tripper.
type transport struct {
	next http.RoundTripper
}

// RoundTrip records one call. It never reads the request body or the response body, and it
// returns exactly what the wrapped round tripper returned.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, end := wlog.StartCall(req.Context(), callOf(req))
	req = req.WithContext(ctx)
	propagate.Inject(ctx, propagate.HeaderCarrier(req.Header))

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		end(wlog.CallResult{Err: err, ErrCode: errorCode(err)})
		return resp, err
	}
	end(wlog.CallResult{Status: strconv.Itoa(resp.StatusCode)})
	return resp, err
}

// routeKey is the context key that carries the route template of one outbound call.
type routeKey struct{}

// WithRoute names the route template of one outbound call, so the target reads
// "host/template". The template is the shape an API documents, such as "/orders/{id}".
func WithRoute(ctx context.Context, route string) context.Context {
	return context.WithValue(ctx, routeKey{}, route)
}

// callOf describes one request for the calls list.
func callOf(req *http.Request) wlog.Call {
	return wlog.Call{
		Kind:      "http",
		System:    "http",
		Operation: req.Method,
		Target:    targetOf(req),
	}
}

// targetOf returns the host of one request, and the route template when the caller named
// one.
func targetOf(req *http.Request) string {
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}
	if route, ok := req.Context().Value(routeKey{}).(string); ok && route != "" {
		return host + route
	}
	return host
}

// errorCode names the failure of one call, and it never holds the error text.
func errorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "transport"
}
