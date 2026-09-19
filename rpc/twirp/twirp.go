// Package wlogtwirp adapts the work kit to Twirp, so one Twirp call gives one wide event
// on the server side and one call record on the client side.
//
// Read top to bottom: ServerHooks completes the request event that the net/http middleware
// starts around the Twirp handler. RequestRouted names the call, Error keeps the Twirp
// error, and ResponseSent writes the code. ClientHooks record one call through
// wlog.StartCall.
//
// This is the whole setup on the server:
//
//	handler := service.NewServer(svc, twirp.WithServerHooks(wlogtwirp.ServerHooks()))
//	http.ListenAndServe(":8080", wlogstd.Middleware(log)(handler))
//
// and on the client:
//
//	client := service.NewProtobufClient(url, http.DefaultClient, twirp.WithClientHooks(wlogtwirp.ClientHooks()))
//
// The server hooks add to the event the net/http middleware started, so the middleware is
// not optional.
package wlogtwirp

import (
	"context"
	"net/http"
	"strings"

	"github.com/twitchtv/twirp"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Option configures the hooks.
type Option func(*options)

// options holds the resolved settings of the client hooks.
type options struct {
	propagateTrace bool
}

// PropagateTrace writes traceparent and tracestate into the request headers of a client
// call. The default is true. Turn it off when another hook owns injection.
func PropagateTrace(on bool) Option {
	return func(o *options) { o.propagateTrace = on }
}

// serverCallKey is the context key that carries the state of one server call.
type serverCallKey struct{}

// serverCall holds the Twirp error of one server call between Error and ResponseSent.
type serverCall struct {
	err twirp.Error
}

// ServerHooks returns the hooks that complete the request event of one Twirp server call.
// Register them with twirp.WithServerHooks.
func ServerHooks(opts ...Option) *twirp.ServerHooks {
	return &twirp.ServerHooks{
		RequestRouted: func(ctx context.Context) (context.Context, error) {
			ctx = context.WithValue(ctx, serverCallKey{}, &serverCall{})
			pkg, _ := twirp.PackageName(ctx)
			service, _ := twirp.ServiceName(ctx)
			method, _ := twirp.MethodName(ctx)
			wlog.Set(ctx, "kind", string(work.KindRPC))
			wlog.SetGroup(ctx, "rpc",
				"system", "twirp", "package", pkg, "service", service, "method", method)
			return ctx, nil
		},
		Error: func(ctx context.Context, err twirp.Error) context.Context {
			if call, ok := ctx.Value(serverCallKey{}).(*serverCall); ok {
				call.err = err
			}
			return ctx
		},
		ResponseSent: func(ctx context.Context) {
			code := "OK"
			if call, ok := ctx.Value(serverCallKey{}).(*serverCall); ok && call.err != nil {
				code = workCode(call.err.Code())
				wlog.Error(ctx, call.err)
			}
			wlog.SetGroup(ctx, "rpc", "status_code", code)
		},
	}
}

// clientCallKey is the context key that carries the end func of one client call.
type clientCallKey struct{}

// ClientHooks returns the hooks that record one call per Twirp client call. Register them
// with twirp.WithClientHooks.
//
// The Error hook accepts a context with no prepared request before it, so a call that
// fails before the request exists reports nothing.
func ClientHooks(opts ...Option) *twirp.ClientHooks {
	cfg := options{propagateTrace: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &twirp.ClientHooks{
		RequestPrepared: func(ctx context.Context, req *http.Request) (context.Context, error) {
			ctx, end := wlog.StartCall(ctx, callOf(req))
			ctx = context.WithValue(ctx, clientCallKey{}, end)
			if cfg.propagateTrace {
				propagate.Inject(ctx, propagate.HeaderCarrier(req.Header))
			}
			return ctx, nil
		},
		ResponseReceived: func(ctx context.Context) {
			if end, ok := ctx.Value(clientCallKey{}).(func(wlog.CallResult)); ok {
				end(wlog.CallResult{Status: "OK"})
			}
		},
		Error: func(ctx context.Context, err twirp.Error) {
			end, ok := ctx.Value(clientCallKey{}).(func(wlog.CallResult))
			if !ok {
				return
			}
			code := workCode(err.Code())
			end(wlog.CallResult{Status: code, Err: err, ErrCode: code})
		},
	}
}

// twirpPath is the path prefix every Twirp route carries.
const twirpPath = "/twirp/"

// callOf describes one outgoing call for the calls list.
func callOf(req *http.Request) wlog.Call {
	return wlog.Call{
		Kind:      "rpc",
		System:    "twirp",
		Operation: strings.TrimPrefix(req.URL.Path, twirpPath),
		Target:    req.URL.Host,
	}
}

// workCode turns one Twirp code name into the work table name, so a reader sees one
// vocabulary across every RPC library.
func workCode(code twirp.ErrorCode) string {
	parts := strings.Split(string(code), "_")
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}
