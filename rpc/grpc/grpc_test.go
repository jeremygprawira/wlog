// This file drives the gRPC adapter over bufconn, with a hand-written service and a
// pass-through codec, so the tests need no generated code. It runs the work suite's
// scenarios for the rpc kind on the server side, and the calls suite's shape on the client
// side.
package wloggrpc_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/propagate"
	wloggrpc "github.com/jeremygprawira/wlog/rpc/grpc"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestGRPC_C2_UnaryFields proves that one unary call gives one rpc event with the fields
// of the work table.
func TestGRPC_C2_UnaryFields(t *testing.T) {
	log, rec := wlogtest.New(t)
	lis := newListener(t, echo(func(context.Context, []byte) ([]byte, error) { return []byte("ok"), nil }),
		wloggrpc.ServerOptions(log)...)
	conn := dial(t, lis)

	var reply []byte
	if err := conn.Invoke(context.Background(), "/probe.Probe/Echo", []byte("hi"), &reply); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if rec.Count() != 1 {
		t.Fatalf("events = %d, want 1", rec.Count())
	}
	got := rec.Last()
	if got["kind"] != "rpc" {
		t.Errorf("kind = %v, want rpc", got["kind"])
	}
	if got["operation"] != "probe.Probe/Echo" {
		t.Errorf("operation = %v, want probe.Probe/Echo", got["operation"])
	}
	if got["level"] != "info" || got["outcome"] != "success" {
		t.Errorf("level/outcome = %v/%v, want info/success", got["level"], got["outcome"])
	}
	fields, _ := got["rpc"].(map[string]any)
	for key, want := range map[string]any{
		"system": "grpc", "service": "probe.Probe", "method": "Echo", "status_code": "OK",
	} {
		if fields[key] != want {
			t.Errorf("rpc.%s = %v, want %v", key, fields[key], want)
		}
	}
}

// TestGRPC_C2_HandlerErrorWarns proves that a status error gives level warn for a client
// code, and error for a server code.
func TestGRPC_C2_HandlerErrorWarns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  codes.Code
		level string
	}{
		{"client", codes.InvalidArgument, "warn"},
		{"server", codes.Internal, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			lis := newListener(t, echo(func(context.Context, []byte) ([]byte, error) {
				return nil, status.Error(tc.code, "the handler failed")
			}), wloggrpc.ServerOptions(log)...)
			conn := dial(t, lis)

			var reply []byte
			err := conn.Invoke(context.Background(), "/probe.Probe/Echo", []byte("hi"), &reply)
			if status.Code(err) != tc.code {
				t.Fatalf("code = %v, want %v", status.Code(err), tc.code)
			}
			if rec.Last()["level"] != tc.level {
				t.Errorf("level = %v, want %s", rec.Last()["level"], tc.level)
			}
			fields, _ := rec.Last()["rpc"].(map[string]any)
			if fields["status_code"] != tc.code.String() {
				t.Errorf("rpc.status_code = %v, want %v", fields["status_code"], tc.code)
			}
		})
	}
}

// TestGRPC_C2_PanicContinues proves that a panicking handler emits one event with a stack,
// and that the panic continues, because gRPC core has no recovery.
func TestGRPC_C2_PanicContinues(t *testing.T) {
	log, rec := wlogtest.New(t)
	interceptor := wloggrpc.UnaryServerInterceptor(log)

	defer func() {
		value := recover()
		if value != "boom" {
			t.Errorf("recovered %v, want the panic value", value)
		}
		if rec.Count() != 1 {
			t.Fatalf("events = %d, want 1", rec.Count())
		}
		info, _ := rec.Last()["error"].(map[string]any)
		if info == nil || info["stack"] == "" {
			t.Errorf("error = %v, want a stack", rec.Last()["error"])
		}
		fields, _ := rec.Last()["rpc"].(map[string]any)
		if fields["status_code"] != "Unknown" {
			t.Errorf("rpc.status_code = %v, want Unknown", fields["status_code"])
		}
	}()

	_, _ = interceptor(context.Background(), []byte("hi"),
		&grpc.UnaryServerInfo{FullMethod: "/probe.Probe/Echo"},
		func(context.Context, any) (any, error) { panic("boom") })
	t.Error("the panic did not continue")
}

// TestGRPC_C3_StatusDetails proves that the status details land in the event error.
func TestGRPC_C3_StatusDetails(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithErrorExtractor(wloggrpc.Extractor()))
	lis := newListener(t, echo(func(context.Context, []byte) ([]byte, error) {
		st := status.New(codes.InvalidArgument, "bad email")
		st, err := st.WithDetails(
			&errdetails.ErrorInfo{Reason: "ORDER_INVALID", Domain: "orders", Metadata: map[string]string{"field": "email"}},
			&errdetails.LocalizedMessage{Locale: "en", Message: "The email is not valid"},
			&errdetails.Help{Links: []*errdetails.Help_Link{{Url: "https://example.com/email"}}},
			&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{{Field: "email"}}},
			&errdetails.RetryInfo{RetryDelay: nil},
		)
		if err != nil {
			return nil, err
		}
		return nil, st.Err()
	}), wloggrpc.ServerOptions(log)...)
	conn := dial(t, lis)

	var reply []byte
	_ = conn.Invoke(context.Background(), "/probe.Probe/Echo", []byte("hi"), &reply)

	info, _ := rec.Last()["error"].(map[string]any)
	if info == nil {
		t.Fatalf("error = %v, want the status details", rec.Last()["error"])
	}
	for key, want := range map[string]string{
		"code":    "ORDER_INVALID",
		"message": "The email is not valid",
		"why":     "bad email",
		"link":    "https://example.com/email",
	} {
		if info[key] != want {
			t.Errorf("error.%s = %v, want %v", key, info[key], want)
		}
	}
	if info["fix"] == nil {
		t.Error("error.fix is missing, want the bad request fix")
	}
	data, _ := info["data"].(map[string]any)
	if data["domain"] != "orders" {
		t.Errorf("error.data.domain = %v, want orders", data["domain"])
	}
	if _, present := data["field_violations"]; !present {
		t.Errorf("error.data = %v, want the field violations", data)
	}
	if _, present := data["retry_delay_ms"]; present {
		t.Errorf("error.data = %v, want no retry delay for a nil RetryDelay", data)
	}
	if _, present := info["internal"]; present {
		t.Errorf("error.internal = %v, want DebugInfo never read", info["internal"])
	}
}

// TestGRPC_C3_UnknownMethod proves that an unknown method gives one event with the
// Unimplemented status.
func TestGRPC_C3_UnknownMethod(t *testing.T) {
	log, rec := wlogtest.New(t)
	lis := newListener(t, echo(nil), append(wloggrpc.ServerOptions(log),
		wloggrpc.UnknownServiceHandler(log))...)
	conn := dial(t, lis)

	var reply []byte
	err := conn.Invoke(context.Background(), "/probe.Nope/Nope", []byte("hi"), &reply)
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("code = %v, want Unimplemented", status.Code(err))
	}

	if rec.Count() != 1 {
		t.Fatalf("events = %d, want 1", rec.Count())
	}
	fields, _ := rec.Last()["rpc"].(map[string]any)
	if fields["method"] != "Nope" {
		t.Errorf("rpc.method = %v, want Nope", fields["method"])
	}
	if fields["status_code"] != "Unimplemented" {
		t.Errorf("rpc.status_code = %v, want Unimplemented", fields["status_code"])
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error, because Unimplemented is a server code", rec.Last()["level"])
	}
}

// TestGRPC_C2_StreamCounts proves that one streaming call gives one event with the stream
// flag and the message counts, which the wrapper counts under concurrency.
func TestGRPC_C2_StreamCounts(t *testing.T) {
	log, rec := wlogtest.New(t)
	lis := newListener(t, service{chat: echoStream}, wloggrpc.ServerOptions(log)...)
	conn := dial(t, lis)

	stream, err := conn.NewStream(context.Background(),
		&grpc.StreamDesc{StreamName: "Chat", ServerStreams: true, ClientStreams: true},
		"/probe.Probe/Chat")
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := stream.SendMsg([]byte("ping")); err != nil {
			t.Fatalf("SendMsg: %v", err)
		}
		var reply []byte
		if err := stream.RecvMsg(&reply); err != nil {
			t.Fatalf("RecvMsg: %v", err)
		}
	}
	_ = stream.CloseSend()
	var last []byte
	if err := stream.RecvMsg(&last); !errors.Is(err, io.EOF) {
		t.Fatalf("final RecvMsg = %v, want io.EOF", err)
	}

	fields, _ := rec.Last()["rpc"].(map[string]any)
	if fields["stream"] != true {
		t.Errorf("rpc.stream = %v, want true", fields["stream"])
	}
	if !conformance.Equal(fields["messages_sent"], 3) {
		t.Errorf("rpc.messages_sent = %v, want 3", fields["messages_sent"])
	}
	if !conformance.Equal(fields["messages_received"], 3) {
		t.Errorf("rpc.messages_received = %v, want 3", fields["messages_received"])
	}
}

// TestGRPC_C2_PluginsRunOnce proves that a plugin's Starter and Finisher each run once
// for one RPC.
func TestGRPC_C2_PluginsRunOnce(t *testing.T) {
	plugin := &counterPlugin{}
	log, _ := wlogtest.New(t, wlog.WithPlugins(plugin))
	lis := newListener(t, echo(func(context.Context, []byte) ([]byte, error) { return nil, nil }),
		wloggrpc.ServerOptions(log)...)
	conn := dial(t, lis)

	var reply []byte
	_ = conn.Invoke(context.Background(), "/probe.Probe/Echo", []byte("hi"), &reply)

	if plugin.starts != 1 {
		t.Errorf("Starter ran %d times, want 1", plugin.starts)
	}
	if plugin.finishes != 1 {
		t.Errorf("Finisher ran %d times, want 1", plugin.finishes)
	}
}

// TestGRPC_C2_ClientCallRecord proves that one client call gives one record with the rpc
// fields and a duration.
func TestGRPC_C2_ClientCallRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	lis := newListener(t, echo(func(context.Context, []byte) ([]byte, error) { return []byte("ok"), nil }))
	conn := dial(t, lis, wloggrpc.DialOptions()...)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	var reply []byte
	if err := conn.Invoke(ctx, "/probe.Probe/Echo", []byte("hi"), &reply); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	for key, want := range map[string]any{
		"kind": "rpc", "system": "grpc", "operation": "/probe.Probe/Echo", "status": "OK",
	} {
		if record[key] != want {
			t.Errorf("call %s = %v, want %v", key, record[key], want)
		}
	}
	if _, present := record["duration_ms"]; !present {
		t.Error("call duration_ms is missing")
	}
}

// TestGRPC_C2_ClientFailedCall proves that a failed call records its code and never the
// raw error text, and that the app's error value is unchanged.
func TestGRPC_C2_ClientFailedCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	lis := newListener(t, echo(func(context.Context, []byte) ([]byte, error) {
		return nil, status.Error(codes.NotFound, "no such order")
	}))
	conn := dial(t, lis, wloggrpc.DialOptions()...)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	var reply []byte
	err := conn.Invoke(ctx, "/probe.Probe/Echo", []byte("hi"), &reply)
	end()
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", status.Code(err))
	}

	calls, _ := rec.Last()["calls"].([]any)
	record, _ := calls[0].(map[string]any)
	info, _ := record["error"].(map[string]any)
	if info["code"] != "NotFound" {
		t.Errorf("call error.code = %v, want NotFound", info["code"])
	}
	if _, present := info["message"]; present {
		t.Errorf("call error.message = %v, want no raw error text", info["message"])
	}
}

// TestGRPC_C2_ClientPropagatesTrace proves that the client sends a traceparent with the
// span id of the call, and that PropagateTrace(false) sends none.
func TestGRPC_C2_ClientPropagatesTrace(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []wloggrpc.Option
		want bool
	}{
		{"default", nil, true},
		{"off", []wloggrpc.Option{wloggrpc.PropagateTrace(false)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var incoming metadata.MD
			lis := newListener(t, echo(func(ctx context.Context, _ []byte) ([]byte, error) {
				incoming, _ = metadata.FromIncomingContext(ctx)
				return nil, nil
			}))
			conn := dial(t, lis, wloggrpc.DialOptions(tc.opts...)...)

			ctx := propagate.Extract(context.Background(), traceCarrier{trace: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"})
			var reply []byte
			if err := conn.Invoke(ctx, "/probe.Probe/Echo", []byte("hi"), &reply); err != nil {
				t.Fatalf("Invoke: %v", err)
			}

			values := incoming.Get("traceparent")
			sent := ""
			if len(values) > 0 {
				sent = values[0]
			}
			if tc.want && sent == "" {
				t.Fatal("the client sent no traceparent")
			}
			if !tc.want && sent != "" {
				t.Errorf("traceparent = %q, want none", sent)
			}
			if tc.want && !strings.Contains(sent, "4bf92f3577b34da6a3ce929d0e0e4736") {
				t.Errorf("traceparent = %q, want the incoming trace id", sent)
			}
		})
	}
}

// TestGRPC_C2_ClientNoEvent proves that a call outside a unit of work records nothing and
// reports nothing.
func TestGRPC_C2_ClientNoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	lis := newListener(t, echo(func(context.Context, []byte) ([]byte, error) { return nil, nil }))
	conn := dial(t, lis, wloggrpc.DialOptions()...)

	var reply []byte
	if err := conn.Invoke(context.Background(), "/probe.Probe/Echo", []byte("hi"), &reply); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// echo builds a service whose Echo method calls fn.
func echo(fn func(context.Context, []byte) ([]byte, error)) service {
	return service{echo: fn}
}

// service is the probe service of the tests.
type service struct {
	echo func(context.Context, []byte) ([]byte, error)
	chat func(grpc.ServerStream) error
}

// Echo answers one unary call.
func (s service) Echo(ctx context.Context, req []byte) ([]byte, error) {
	if s.echo == nil {
		return nil, nil
	}
	return s.echo(ctx, req)
}

// Chat answers one streaming call.
func (s service) Chat(stream grpc.ServerStream) error {
	if s.chat == nil {
		return nil
	}
	return s.chat(stream)
}

// echoStream echoes every received message until the client closes the stream.
func echoStream(stream grpc.ServerStream) error {
	for {
		var msg []byte
		if err := stream.RecvMsg(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := stream.SendMsg(msg); err != nil {
			return err
		}
	}
}

// counterPlugin counts the Starter and Finisher calls of one RPC.
type counterPlugin struct {
	starts   int
	finishes int
}

// Name satisfies the Plugin contract.
func (*counterPlugin) Name() string { return "counter" }

// OnStart counts one start.
func (p *counterPlugin) OnStart(ctx context.Context, _ string) context.Context {
	p.starts++
	return ctx
}

// OnFinish counts one finish.
func (p *counterPlugin) OnFinish(context.Context, wlog.Event) { p.finishes++ }

// passCodec carries message bytes through, so the tests need no protobuf code.
type passCodec struct{}

// Marshal returns the message bytes unchanged.
func (passCodec) Marshal(v any) ([]byte, error) {
	body, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("passCodec: unexpected type %T", v)
	}
	return body, nil
}

// Unmarshal copies the wire bytes into the message.
func (passCodec) Unmarshal(data []byte, v any) error {
	out, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("passCodec: unexpected type %T", v)
	}
	*out = append([]byte(nil), data...)
	return nil
}

// Name names the codec.
func (passCodec) Name() string { return "pass" }

// probeDesc describes the probe service.
var probeDesc = grpc.ServiceDesc{
	ServiceName: "probe.Probe",
	HandlerType: (*probeService)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Echo",
		Handler:    echoHandler,
	}},
	Streams: []grpc.StreamDesc{{
		StreamName:    "Chat",
		Handler:       chatHandler,
		ServerStreams: true,
		ClientStreams: true,
	}},
	Metadata: "probe",
}

// probeService is the handler type of the probe service.
type probeService interface {
	Echo(context.Context, []byte) ([]byte, error)
	Chat(grpc.ServerStream) error
}

// echoHandler serves one unary call through the interceptor chain.
func echoHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	var req []byte
	if err := dec(&req); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(probeService).Echo(ctx, req)
	}
	info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/probe.Probe/Echo"}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(probeService).Echo(ctx, req.([]byte))
	}
	return interceptor(ctx, req, info, handler)
}

// chatHandler serves one streaming call through the interceptor chain.
func chatHandler(srv any, stream grpc.ServerStream) error {
	return srv.(probeService).Chat(stream)
}

// newListener starts a bufconn server with impl and the given options.
func newListener(t *testing.T, impl probeService, opts ...grpc.ServerOption) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	all := append([]grpc.ServerOption{grpc.ForceServerCodec(passCodec{})}, opts...)
	server := grpc.NewServer(all...)
	server.RegisterService(&probeDesc, impl)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)
	return lis
}

// dial returns a client connection to lis with the given options.
func dial(t *testing.T, lis *bufconn.Listener, opts ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()
	all := append([]grpc.DialOption{
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(passCodec{})),
	}, opts...)
	conn, err := grpc.NewClient("passthrough:///bufnet", all...)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// traceCarrier is a read-only propagate carrier over one traceparent value.
type traceCarrier struct {
	trace string
}

// Get returns the traceparent value.
func (c traceCarrier) Get(key string) string {
	if key == "traceparent" {
		return c.trace
	}
	return ""
}

// Set does nothing, because the carrier is read-only.
func (traceCarrier) Set(string, string) {}

// Keys returns the carrier keys.
func (traceCarrier) Keys() []string { return []string{"traceparent"} }
