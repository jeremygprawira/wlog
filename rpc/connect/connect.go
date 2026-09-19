// Package wlogconnect adapts the work kit to ConnectRPC, so one Connect call gives one
// wide event on the server side and one call record on the client side.
//
// Read top to bottom: Interceptor serves both sides, told apart by Spec().IsClient. A
// server call opens one rpc unit through work, records the status through the shared table,
// and emits at End. A client call records one call through wlog.StartCall, and it adds
// context values only, because Connect forbids a new client context inside an interceptor.
//
// Connect answers some requests before an interceptor runs: a bad method gives 405, an
// unsupported content type gives 415, and a malformed body gives 400. Wrap the handler with
// the net/http middleware (wlogstd.Middleware) as well, and those requests still become
// events, with the request kind.
package wlogconnect

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"connectrpc.com/connect"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Option configures the interceptor.
type Option func(*options)

// options holds the resolved settings of one interceptor.
type options struct {
	propagateTrace bool
}

// PropagateTrace writes traceparent and tracestate into the request headers of a client
// call. The default is true. Turn it off when another interceptor, such as otelconnect,
// owns injection.
func PropagateTrace(on bool) Option {
	return func(o *options) { o.propagateTrace = on }
}

// Interceptor returns the interceptor that serves both sides of a Connect call. Register
// it with connect.WithInterceptors on the handler and on the client.
func Interceptor(log *wlog.Logger, opts ...Option) connect.Interceptor {
	cfg := options{propagateTrace: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &interceptor{log: log, cfg: cfg}
}

// interceptor serves both sides of one Connect call.
type interceptor struct {
	log *wlog.Logger
	cfg options
}

// WrapUnary wraps one unary call on the side that Spec names.
func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return i.unaryClient(next, ctx, req)
		}
		return i.unaryServer(next, ctx, req)
	}
}

// unaryServer gives one incoming unary call one event.
func (i *interceptor) unaryServer(next connect.UnaryFunc, ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
	ctx, h := work.Start(ctx, i.log, unitFrom(req.Spec(), req.Peer(), req.Header(), false))
	res, err := next(ctx, req)
	code := codeName(codeOf(err))
	h.Status(code, work.ClassOf(work.KindRPC, code))
	h.End(err)
	return res, err
}

// unaryClient records one outgoing unary call, and it adds context values only.
func (i *interceptor) unaryClient(next connect.UnaryFunc, ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
	ctx, end := wlog.StartCall(ctx, clientCall(req.Spec(), req.Peer()))
	if i.cfg.propagateTrace {
		propagate.Inject(ctx, propagate.HeaderCarrier(req.Header()))
	}
	res, err := next(ctx, req)
	code := codeName(codeOf(err))
	end(wlog.CallResult{Status: code, Err: err, ErrCode: errorCode(code)})
	return res, err
}

// WrapStreamingHandler wraps one incoming streaming call, and counts its messages.
func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, h := work.Start(ctx, i.log, unitFrom(conn.Spec(), conn.Peer(), conn.RequestHeader(), true))
		wrapped := &handlerConn{StreamingHandlerConn: conn}
		err := next(ctx, wrapped)
		h.Set("messages_sent", wrapped.sent.Load())
		h.Set("messages_received", wrapped.received.Load())
		code := codeName(codeOf(err))
		h.Status(code, work.ClassOf(work.KindRPC, code))
		h.End(err)
		return err
	}
}

// WrapStreamingClient wraps one outgoing streaming call. The call ends at the first of the
// last receive and the response close.
func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		ctx, end := wlog.StartCall(ctx, clientCall(spec, connect.Peer{}))
		conn := next(ctx, spec)
		if i.cfg.propagateTrace {
			// The request headers exist once the connection does, and the first send
			// has not run yet.
			propagate.Inject(ctx, propagate.HeaderCarrier(conn.RequestHeader()))
		}
		return &clientConn{StreamingClientConn: conn, end: end}
	}
}

// unitFrom builds the unit of work of one incoming call.
func unitFrom(spec connect.Spec, peer connect.Peer, header http.Header, stream bool) work.Unit {
	service, method := splitProcedure(spec.Procedure)
	fields := map[string]any{"system": "connect", "service": service, "method": method}
	if peer.Protocol != "" {
		fields["protocol"] = peer.Protocol
	}
	if peer.Addr != "" {
		fields["peer"] = peer.Addr
	}
	if stream {
		fields["stream"] = true
	}
	return work.Unit{Kind: work.KindRPC, Fields: fields, Carrier: propagate.HeaderCarrier(header)}
}

// clientCall describes one outgoing call for the calls list.
func clientCall(spec connect.Spec, peer connect.Peer) wlog.Call {
	return wlog.Call{Kind: "rpc", System: "connect", Operation: spec.Procedure, Target: peer.Addr}
}

// splitProcedure splits one procedure into the service and the method, at the last slash.
func splitProcedure(procedure string) (service, method string) {
	name := strings.TrimPrefix(procedure, "/")
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[:index], name[index+1:]
	}
	return "", name
}

// codeOf returns the Connect code of one finished call.
//
// connect.CodeOf is never used, because it reports Unknown for a nil error and for a
// canceled context. A plain context error keeps its own code, as the wire maps it.
func codeOf(err error) connect.Code {
	if err == nil || errors.Is(err, io.EOF) {
		return codeOK
	}
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code()
	}
	if errors.Is(err, context.Canceled) {
		return connect.CodeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.CodeDeadlineExceeded
	}
	return connect.CodeUnknown
}

// errorCode returns the code of a failed call, and an empty string for a call that
// succeeded, so a success carries no error object.
func errorCode(code string) string {
	if code == codeNames[codeOK] {
		return ""
	}
	return code
}

// handlerConn wraps one streaming handler connection, so the interceptor counts every
// message. The read side can run at the same time as the write side, so the counters are
// atomic.
type handlerConn struct {
	connect.StreamingHandlerConn
	sent     atomic.Int64
	received atomic.Int64
}

// Send counts one message the server sent.
func (c *handlerConn) Send(msg any) error {
	err := c.StreamingHandlerConn.Send(msg)
	if err == nil {
		c.sent.Add(1)
	}
	return err
}

// Receive counts one message the server received.
func (c *handlerConn) Receive(msg any) error {
	err := c.StreamingHandlerConn.Receive(msg)
	if err == nil {
		c.received.Add(1)
	}
	return err
}

// clientConn wraps one streaming client connection, so the interceptor ends the call
// once, at the first of the last receive and the response close.
type clientConn struct {
	connect.StreamingClientConn
	end  func(wlog.CallResult)
	once sync.Once
}

// Receive ends the call when the stream ends. An io.EOF is a success, not a failure.
func (c *clientConn) Receive(msg any) error {
	err := c.StreamingClientConn.Receive(msg)
	if err != nil {
		c.finish(err)
	}
	return err
}

// CloseResponse ends the call when the caller closes the response.
func (c *clientConn) CloseResponse() error {
	err := c.StreamingClientConn.CloseResponse()
	c.finish(err)
	return err
}

// finish records the call once.
func (c *clientConn) finish(err error) {
	code := codeName(codeOf(err))
	failure := err
	if errors.Is(err, io.EOF) {
		failure = nil
	}
	c.once.Do(func() {
		c.end(wlog.CallResult{Status: code, Err: failure, ErrCode: errorCode(code)})
	})
}
