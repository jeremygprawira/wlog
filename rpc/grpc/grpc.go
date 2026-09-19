// Package wloggrpc is wlog's gRPC adapter: one wide event per RPC on the server side, and
// one call record per logical call on the client side.
//
// Read top to bottom: ServerOptions installs the unary and the stream interceptor, and
// DialOptions installs the client pair. Both build the rpc group from the full method, and
// both read the status through status.FromError, so the level follows the work table.
// UnknownServiceHandler gives an event to a method that no service declares. Extractor
// turns a status and its details into the ErrorInfo of the event.
//
// Two rules surprise a reader. First, gRPC core never recovers a panic, so a panicking
// handler records the panic, emits the event, and continues the panic. Second, a call to
// an unknown service or method never reaches an interceptor, so install
// UnknownServiceHandler to see those calls.
package wloggrpc

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Option configures one interceptor pair.
type Option func(*options)

// options holds the resolved settings of one pair of interceptors.
type options struct {
	propagateTrace bool
}

// PropagateTrace writes traceparent and tracestate into outgoing metadata. The default is
// true. Turn it off when an OpenTelemetry stats handler owns injection, so the span id on
// the wire is the OpenTelemetry one.
func PropagateTrace(on bool) Option {
	return func(o *options) { o.propagateTrace = on }
}

// newOptions resolves the options of one interceptor pair.
func newOptions(opts []Option) options {
	cfg := options{propagateTrace: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// ServerOptions returns the gRPC server options that give every RPC one event: a chained
// unary interceptor and a chained stream interceptor. The server reads no option, because
// only the client injects a trace header. The parameter stays, so both constructors read
// the same way.
func ServerOptions(log *wlog.Logger, opts ...Option) []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(UnaryServerInterceptor(log)),
		grpc.ChainStreamInterceptor(StreamServerInterceptor(log)),
	}
}

// DialOptions returns the gRPC dial options that record one call per logical RPC.
func DialOptions(opts ...Option) []grpc.DialOption {
	cfg := newOptions(opts)
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(UnaryClientInterceptor(cfg)),
		grpc.WithChainStreamInterceptor(StreamClientInterceptor(cfg)),
	}
}

// UnknownServiceHandler gives every call to an unknown service or method one event and the
// Unimplemented status. Install it next to ServerOptions, because gRPC core routes such a
// call past the service table and straight to the stream interceptor.
func UnknownServiceHandler(log *wlog.Logger) grpc.ServerOption {
	return grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		method, _ := grpc.Method(stream.Context())
		return status.Errorf(codes.Unimplemented, "unknown service or method: %s", method)
	})
}

// UnaryServerInterceptor returns the interceptor that gives one unary RPC one event, and
// that records the status code and the level from the returned error.
func UnaryServerInterceptor(log *wlog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, h := work.Start(ctx, log, rpcUnit(ctx, info.FullMethod, false))

		var resp any
		var err error
		var recovered any
		var stack string
		func() {
			defer func() {
				if value := recover(); value != nil {
					recovered, stack = value, string(debug.Stack())
				}
			}()
			resp, err = handler(ctx, req)
		}()

		if recovered != nil {
			h.Set("status_code", codes.Unknown.String())
			h.End(&panicError{value: recovered, stack: stack})
			panic(recovered)
		}
		code := codeOf(err)
		h.Status(code, work.ClassOf(work.KindRPC, code))
		h.End(err)
		return resp, err
	}
}

// UnaryClientInterceptor returns the interceptor that records one call for one unary RPC,
// adds trace headers, and hands the app's own error back unchanged.
func UnaryClientInterceptor(cfg options) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, end := wlog.StartCall(ctx, clientCall(cc, method))
		if cfg.propagateTrace {
			ctx = injectTrace(ctx)
		}
		err := invoker(ctx, method, req, reply, cc, opts...)
		code := clientCode(ctx, err)
		end(wlog.CallResult{Status: code, Err: err, ErrCode: errorCode(code)})
		return err
	}
}

// StreamClientInterceptor returns the interceptor that records one call for one streaming
// RPC. The call ends through grpc.OnFinish, which runs once, whenever the stream ends.
func StreamClientInterceptor(cfg options) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx, end := wlog.StartCall(ctx, clientCall(cc, method))
		if cfg.propagateTrace {
			ctx = injectTrace(ctx)
		}
		var once sync.Once
		finish := func(err error) {
			once.Do(func() {
				code := clientCode(ctx, err)
				end(wlog.CallResult{Status: code, Err: err, ErrCode: errorCode(code)})
			})
		}
		all := append(append([]grpc.CallOption{}, opts...), grpc.OnFinish(finish))
		stream, err := streamer(ctx, desc, cc, method, all...)
		if err != nil {
			finish(err)
			return nil, err
		}
		return stream, nil
	}
}

// rpcUnit builds the unit of work of one server RPC.
func rpcUnit(ctx context.Context, fullMethod string, stream bool) work.Unit {
	service, method := splitMethod(fullMethod)
	fields := map[string]any{"system": "grpc", "service": service, "method": method}
	if peer := peerOf(ctx); peer != "" {
		fields["peer"] = peer
	}
	if stream {
		fields["stream"] = true
	}
	return work.Unit{
		Kind:    work.KindRPC,
		Fields:  fields,
		Carrier: incomingCarrier{md: incomingMetadata(ctx)},
	}
}

// splitMethod splits a full method into the service and the method, at the last slash.
// The parse is loose, because an old gRPC core can pass a path without a leading slash.
func splitMethod(fullMethod string) (service, method string) {
	name := strings.TrimPrefix(fullMethod, "/")
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[:index], name[index+1:]
	}
	return "", name
}

// peerOf returns the address of the calling peer, and an empty string when gRPC knows
// none.
func peerOf(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil || p.Addr == nil {
		return ""
	}
	return p.Addr.String()
}

// clientCall describes one outgoing RPC for the calls list.
func clientCall(cc *grpc.ClientConn, method string) wlog.Call {
	call := wlog.Call{Kind: "rpc", System: "grpc", Operation: method}
	if cc != nil {
		call.Target = cc.Target()
	}
	return call
}

// codeOf returns the status code name of one finished server call. A plain error goes
// through status.FromContextError, exactly as the gRPC server maps it, so a canceled
// context reports Canceled and not Unknown.
func codeOf(err error) string {
	if err == nil {
		return codes.OK.String()
	}
	if st, ok := status.FromError(err); ok {
		return st.Code().String()
	}
	return status.FromContextError(err).Code().String()
}

// clientCode returns the status code name of one finished client call. A canceled or an
// expired context wins, so a local cancel is not read as a remote failure.
func clientCode(ctx context.Context, err error) string {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return status.FromContextError(ctxErr).Code().String()
	}
	return codeOf(err)
}

// errorCode returns the code of a failed call, and an empty string for a call that
// succeeded, so a success carries no error object.
func errorCode(code string) string {
	if code == codes.OK.String() {
		return ""
	}
	return code
}

// incomingCarrier reads the metadata of one incoming call. A write does nothing, because
// an incoming request is read-only.
type incomingCarrier struct {
	md metadata.MD
}

// Get returns the first value of one metadata key.
func (c incomingCarrier) Get(key string) string {
	if values := c.md.Get(key); len(values) > 0 {
		return values[0]
	}
	return ""
}

// Set does nothing.
func (incomingCarrier) Set(string, string) {}

// Keys returns the metadata keys.
func (c incomingCarrier) Keys() []string {
	out := make([]string, 0, len(c.md))
	for key := range c.md {
		out = append(out, key)
	}
	return out
}

// outgoingCarrier writes the trace headers into one metadata map.
type outgoingCarrier struct {
	md metadata.MD
}

// Get returns an empty string, because an outgoing carrier only writes.
func (outgoingCarrier) Get(string) string { return "" }

// Set writes one metadata value.
func (c outgoingCarrier) Set(key, value string) { c.md.Set(key, value) }

// Keys returns no keys.
func (outgoingCarrier) Keys() []string { return nil }

// incomingMetadata returns the metadata of one incoming context, and an empty map when
// gRPC holds none.
func incomingMetadata(ctx context.Context) metadata.MD {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return metadata.MD{}
	}
	return md
}

// injectTrace appends the trace headers of the context to the outgoing metadata. It keeps
// the metadata the app already set, and it writes nothing when the context carries no
// trace context.
func injectTrace(ctx context.Context) context.Context {
	md := metadata.MD{}
	propagate.Inject(ctx, outgoingCarrier{md: md})
	if len(md) == 0 {
		return ctx
	}
	pairs := make([]string, 0, len(md)*2)
	for key, values := range md {
		for _, value := range values {
			pairs = append(pairs, key, value)
		}
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// panicError carries a recovered panic value and the stack of the moment it was recovered.
// The default extractor of core reads the stack through the Stack method.
type panicError struct {
	value any
	stack string
}

// Error returns the panic value as a message.
func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.value) }

// Stack returns the stack of the panic.
func (e *panicError) Stack() string { return e.stack }
