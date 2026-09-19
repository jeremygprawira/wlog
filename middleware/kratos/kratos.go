// Package wlogkratos adapts http-core to the Kratos HTTP transport, so a Kratos service
// gets the same wide event as a plain net/http app, in one call.
//
// Read top to bottom: Filter starts one event per request through http-core, and it sees
// redirects, 404s, and rejections too. Middleware adds the route Kratos matched, and the
// reason and metadata of a Kratos error. The two calls are the whole setup:
//
//	khttp.NewServer(
//		khttp.Filter(wlogkratos.Filter(log)),
//		khttp.Middleware(wlogkratos.Middleware()),
//	)
//
// A Kratos gRPC server records through the rpc-grpc interceptors instead, with the Kratos
// transport/grpc options UnaryInterceptor and StreamInterceptor.
package wlogkratos

import (
	"context"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
)

// Option is an http-core option, re-exported so a caller needs one import.
type Option = httpcore.Option

// The http-core options this adapter passes through.
var (
	SkipPaths        = httpcore.SkipPaths
	CaptureAll       = httpcore.CaptureAll
	CaptureHeaders   = httpcore.CaptureHeaders
	CaptureBody      = httpcore.CaptureBody
	MaxBodyCapture   = httpcore.MaxBody
	BodyContentTypes = httpcore.BodyTypes
	WithUserFunc     = httpcore.User
)

// routeKey is the request context key that carries the route holder of one request.
type routeKey struct{}

// routeHolder carries the route Kratos matched to the filter that reads it after the
// chain, because Kratos keeps its route outside the request context of the filter.
type routeHolder struct {
	template string
}

// Filter returns the Kratos filter that emits one wide event per request through log. Pass
// it to khttp.Filter, so the filter wraps the whole router and sees every request.
func Filter(log *wlog.Logger, opts ...Option) khttp.FilterFunc {
	routeFn := httpcore.RouteFunc(func(r *http.Request) string {
		if holder, ok := r.Context().Value(routeKey{}).(*routeHolder); ok {
			return holder.template
		}
		return ""
	})
	core := httpcore.NetHTTP(log, append(opts, routeFn)...)
	return func(next http.Handler) http.Handler {
		inner := core(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			holder := &routeHolder{}
			inner.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), routeKey{}, holder)))
		})
	}
}

// Middleware returns the Kratos middleware that completes the open event with the route
// Kratos matched, and with the reason and metadata of a Kratos error. Pass it to
// khttp.Middleware.
func Middleware() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			if holder, ok := ctx.Value(routeKey{}).(*routeHolder); ok {
				holder.template = pathTemplate(ctx)
			}
			reply, err := handler(ctx, req)
			if err != nil {
				reason, metadata := errorDetails(err)
				if reason != "" {
					wlog.SetGroup(ctx, "http", "reason", reason)
				}
				if len(metadata) > 0 {
					wlog.SetGroup(ctx, "http", "metadata", metadata)
				}
				wlog.Error(ctx, err)
			}
			return reply, err
		}
	}
}

// pathTemplate returns the route template Kratos matched, or an empty string.
func pathTemplate(ctx context.Context) string {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return ""
	}
	kt, ok := tr.(khttp.Transporter)
	if !ok {
		return ""
	}
	return kt.PathTemplate()
}

// errorDetails returns the reason and the metadata of one Kratos error.
func errorDetails(err error) (string, map[string]string) {
	se := kerrors.FromError(err)
	return se.Reason, se.Metadata
}
