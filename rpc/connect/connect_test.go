// This file drives the Connect interceptor over httptest, with a hand-written codec, so
// the tests need no generated code. It checks the server fields, the client call record,
// trace propagation, and the malformed body that never reaches an interceptor.
package wlogconnect_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	"github.com/jeremygprawira/wlog/propagate"
	wlogconnect "github.com/jeremygprawira/wlog/rpc/connect"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// procedure is the route every test call uses.
const procedure = "/probe.v1.ProbeService/Echo"

// TestConnect_C2_ServerFields proves that one unary call gives one rpc event with the
// fields of the work table.
func TestConnect_C2_ServerFields(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := newUnaryServer(t, log, func(context.Context, *connect.Request[[]byte]) (*connect.Response[[]byte], error) {
		return connect.NewResponse(bytesOf("ok")), nil
	})

	callUnary(t, server, context.Background())

	got := rec.Last()
	if got["kind"] != "rpc" {
		t.Errorf("kind = %v, want rpc", got["kind"])
	}
	if got["operation"] != "probe.v1.ProbeService/Echo" {
		t.Errorf("operation = %v, want probe.v1.ProbeService/Echo", got["operation"])
	}
	if got["level"] != "info" || got["outcome"] != "success" {
		t.Errorf("level/outcome = %v/%v, want info/success", got["level"], got["outcome"])
	}
	fields, _ := got["rpc"].(map[string]any)
	for key, want := range map[string]any{
		"system": "connect", "service": "probe.v1.ProbeService", "method": "Echo",
		"protocol": "connect", "status_code": "OK",
	} {
		if fields[key] != want {
			t.Errorf("rpc.%s = %v, want %v", key, fields[key], want)
		}
	}
}

// TestConnect_C2_HandlerErrorWarns proves that a client code warns and a server code is an
// error.
func TestConnect_C2_HandlerErrorWarns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  connect.Code
		level string
	}{
		{"client", connect.CodeInvalidArgument, "warn"},
		{"server", connect.CodeInternal, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			server := newUnaryServer(t, log, func(context.Context, *connect.Request[[]byte]) (*connect.Response[[]byte], error) {
				return nil, connect.NewError(tc.code, errors.New("the handler failed"))
			})

			err := callUnary(t, server, context.Background())
			if connect.CodeOf(err) != tc.code {
				t.Fatalf("code = %v, want %v", connect.CodeOf(err), tc.code)
			}
			if rec.Last()["level"] != tc.level {
				t.Errorf("level = %v, want %s", rec.Last()["level"], tc.level)
			}
			fields, _ := rec.Last()["rpc"].(map[string]any)
			if fields["status_code"] != connectCodeName(tc.code) {
				t.Errorf("rpc.status_code = %v, want %v", fields["status_code"], tc.code)
			}
		})
	}
}

// TestConnect_C2_ClientCallRecord proves that one client call gives one record with the
// rpc fields.
func TestConnect_C2_ClientCallRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	server := newUnaryServer(t, log, func(context.Context, *connect.Request[[]byte]) (*connect.Response[[]byte], error) {
		return connect.NewResponse(bytesOf("ok")), nil
	})
	client := newClient(t, server, log)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	_, err := client.CallUnary(ctx, connect.NewRequest(bytesOf("hi")))
	end()
	if err != nil {
		t.Fatalf("CallUnary: %v", err)
	}

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	for key, want := range map[string]any{
		"kind": "rpc", "system": "connect", "operation": procedure, "status": "OK",
	} {
		if record[key] != want {
			t.Errorf("call %s = %v, want %v", key, record[key], want)
		}
	}
}

// TestConnect_C2_ClientPropagatesTrace proves that the client sends a traceparent on the
// request, and that PropagateTrace(false) sends none.
func TestConnect_C2_ClientPropagatesTrace(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []wlogconnect.Option
		want bool
	}{
		{"default", nil, true},
		{"off", []wlogconnect.Option{wlogconnect.PropagateTrace(false)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, _ := wlogtest.New(t)
			server := newUnaryServer(t, log, func(_ context.Context, req *connect.Request[[]byte]) (*connect.Response[[]byte], error) {
				return connect.NewResponse(bytesOf(req.Header().Get("Traceparent"))), nil
			})
			client := newClient(t, server, log, tc.opts...)

			ctx := propagate.Extract(context.Background(), traceCarrier{trace: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"})
			res, err := client.CallUnary(ctx, connect.NewRequest(bytesOf("hi")))
			if err != nil {
				t.Fatalf("CallUnary: %v", err)
			}
			sent := string(*res.Msg)
			if tc.want && !strings.Contains(sent, "4bf92f3577b34da6a3ce929d0e0e4736") {
				t.Errorf("traceparent = %q, want the incoming trace id", sent)
			}
			if !tc.want && sent != "" {
				t.Errorf("traceparent = %q, want none", sent)
			}
		})
	}
}

// TestConnect_C4_MalformedBody proves that a malformed body gives one event with status
// 400 through the net/http middleware, and that no interceptor runs.
func TestConnect_C4_MalformedBody(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := connect.NewUnaryHandler[[]byte, []byte](procedure,
		func(context.Context, *connect.Request[[]byte]) (*connect.Response[[]byte], error) {
			return connect.NewResponse(bytesOf("ok")), nil
		},
		connect.WithInterceptors(wlogconnect.Interceptor(log)),
		connect.WithCodec(rawCodec{}),
	)
	mux := http.NewServeMux()
	mux.Handle("/probe.v1.ProbeService/", handler)
	wrapped := wlogstd.Middleware(log)(mux)

	req := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", recorder.Code)
	}
	if rec.Count() != 1 {
		t.Fatalf("events = %d, want 1", rec.Count())
	}
	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusBadRequest) {
		t.Errorf("http.status = %v, want 400", httpFields["status"])
	}
	if event["kind"] != "request" {
		t.Errorf("kind = %v, want request, because the interceptor never ran", event["kind"])
	}
}

// TestConnect_C2_ServerStreamCounts proves that one streaming call gives one event with
// the stream flag and the message counts.
func TestConnect_C2_ServerStreamCounts(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := connect.NewServerStreamHandler[[]byte, []byte](procedure,
		func(_ context.Context, _ *connect.Request[[]byte], stream *connect.ServerStream[[]byte]) error {
			for i := 0; i < 3; i++ {
				if err := stream.Send(bytesOf("ping")); err != nil {
					return err
				}
			}
			return nil
		},
		connect.WithInterceptors(wlogconnect.Interceptor(log)),
		connect.WithCodec(rawCodec{}),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := connect.NewClient[[]byte, []byte](server.Client(), server.URL+procedure,
		connect.WithCodec(rawCodec{}), connect.WithInterceptors(wlogconnect.Interceptor(log)))

	stream, err := client.CallServerStream(context.Background(), connect.NewRequest(bytesOf("hi")))
	if err != nil {
		t.Fatalf("CallServerStream: %v", err)
	}
	received := 0
	for stream.Receive() {
		_ = stream.Msg()
		received++
	}
	if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Receive: %v", err)
	}
	if received != 3 {
		t.Fatalf("received %d messages, want 3", received)
	}

	fields, _ := rec.Last()["rpc"].(map[string]any)
	if fields["stream"] != true {
		t.Errorf("rpc.stream = %v, want true", fields["stream"])
	}
	if !conformance.Equal(fields["messages_sent"], 3) {
		t.Errorf("rpc.messages_sent = %v, want 3", fields["messages_sent"])
	}
	if !conformance.Equal(fields["messages_received"], 1) {
		t.Errorf("rpc.messages_received = %v, want 1", fields["messages_received"])
	}
}

// newUnaryServer starts one Connect unary handler over httptest with the interceptor.
func newUnaryServer(t *testing.T, log *wlog.Logger, fn func(context.Context, *connect.Request[[]byte]) (*connect.Response[[]byte], error)) *httptest.Server {
	t.Helper()
	handler := connect.NewUnaryHandler[[]byte, []byte](procedure, fn,
		connect.WithInterceptors(wlogconnect.Interceptor(log)),
		connect.WithCodec(rawCodec{}),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// newClient returns a Connect client for one server, with the interceptor on the client
// side too.
func newClient(t *testing.T, server *httptest.Server, log *wlog.Logger, opts ...wlogconnect.Option) *connect.Client[[]byte, []byte] {
	t.Helper()
	return connect.NewClient[[]byte, []byte](server.Client(), server.URL+procedure,
		connect.WithCodec(rawCodec{}),
		connect.WithInterceptors(wlogconnect.Interceptor(log, opts...)),
	)
}

// callUnary makes one unary call and returns its error.
func callUnary(t *testing.T, server *httptest.Server, ctx context.Context) error {
	t.Helper()
	client := connect.NewClient[[]byte, []byte](server.Client(), server.URL+procedure, connect.WithCodec(rawCodec{}))
	_, err := client.CallUnary(ctx, connect.NewRequest(bytesOf("hi")))
	return err
}

// connectCodeName returns the work table name of one code, which the test checks against
// the event.
func connectCodeName(code connect.Code) string {
	switch code {
	case connect.CodeInvalidArgument:
		return "InvalidArgument"
	case connect.CodeInternal:
		return "Internal"
	}
	return code.String()
}

// bytesOf returns a pointer to one message.
func bytesOf(body string) *[]byte {
	msg := []byte(body)
	return &msg
}

// rawCodec carries one byte slice, so the tests need no generated code.
type rawCodec struct{}

// Name names the codec.
func (rawCodec) Name() string { return "raw" }

// Marshal returns the message bytes.
func (rawCodec) Marshal(v any) ([]byte, error) {
	switch msg := v.(type) {
	case []byte:
		return msg, nil
	case *[]byte:
		return *msg, nil
	}
	return nil, fmt.Errorf("rawCodec: cannot marshal %T", v)
}

// Unmarshal copies the wire bytes into the message.
func (rawCodec) Unmarshal(data []byte, v any) error {
	if msg, ok := v.(*[]byte); ok {
		*msg = append([]byte(nil), data...)
		return nil
	}
	return fmt.Errorf("rawCodec: cannot unmarshal into %T", v)
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
