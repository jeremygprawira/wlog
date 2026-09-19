// This file holds the server side of one streaming RPC: the interceptor, and the wrapper
// that carries the event on the handler context and counts every message.
package wloggrpc

import (
	"context"
	"runtime/debug"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// StreamServerInterceptor returns the interceptor that gives one streaming RPC one event,
// and that counts the messages each side sent.
func StreamServerInterceptor(log *wlog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, h := work.Start(ss.Context(), log, rpcUnit(ss.Context(), info.FullMethod, true))
		wrapped := &serverStream{ServerStream: ss, ctx: ctx}

		var err error
		var recovered any
		var stack string
		func() {
			defer func() {
				if value := recover(); value != nil {
					recovered, stack = value, string(debug.Stack())
				}
			}()
			err = handler(srv, wrapped)
		}()

		h.Set("messages_sent", wrapped.sent.Load())
		h.Set("messages_received", wrapped.received.Load())
		if recovered != nil {
			h.Set("status_code", codes.Unknown.String())
			h.End(&panicError{value: recovered, stack: stack})
			panic(recovered)
		}
		code := codeOf(err)
		h.Status(code, work.ClassOf(work.KindRPC, code))
		h.End(err)
		return err
	}
}

// serverStream wraps one grpc.ServerStream, so the handler context carries the event and
// the interceptor counts every message. gRPC lets SendMsg and RecvMsg run at the same
// time on different goroutines, so the counters are atomic.
type serverStream struct {
	grpc.ServerStream
	ctx      context.Context
	sent     atomic.Int64
	received atomic.Int64
}

// Context returns the context that carries the event.
func (s *serverStream) Context() context.Context { return s.ctx }

// SendMsg counts one message the server sent.
func (s *serverStream) SendMsg(m any) error {
	err := s.ServerStream.SendMsg(m)
	if err == nil {
		s.sent.Add(1)
	}
	return err
}

// RecvMsg counts one message the server received.
func (s *serverStream) RecvMsg(m any) error {
	err := s.ServerStream.RecvMsg(m)
	if err == nil {
		s.received.Add(1)
	}
	return err
}
