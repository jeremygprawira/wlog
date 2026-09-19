// This file runs the http conformance suite against the Kratos HTTP adapter, checks the
// reason and metadata a Kratos error carries, and proves that a Kratos gRPC server records
// through the rpc-grpc interceptors.
package wlogkratos_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
	kgrpc "github.com/go-kratos/kratos/v2/transport/grpc"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogkratos "github.com/jeremygprawira/wlog/middleware/kratos"
	wloggrpc "github.com/jeremygprawira/wlog/rpc/grpc"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestKratos_Conformance proves that the Kratos HTTP adapter passes every scenario of the
// http suite.
func TestKratos_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, kratosFactory{})
}

// kratosFactory builds the adapter around a Kratos server of the suite's route table.
type kratosFactory struct{}

// Build returns the Kratos server with the filter and the middleware. The route handlers
// run the suite's net/http handlers inside the Kratos middleware chain, the way a
// generated Kratos service does.
func (kratosFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	opts := []wlogkratos.Option{wlogkratos.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogkratos.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogkratos.MaxBodyCapture(settings.MaxBody))
	}
	srv := khttp.NewServer(
		khttp.Filter(wlogkratos.Filter(log, opts...)),
		khttp.Middleware(wlogkratos.Middleware()),
	)
	r := srv.Route("/")
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
		r.Handle(method, "/ok", kratosHandler(routes.OK))
		r.Handle(method, "/orders/{id}", kratosHandler(routes.Order))
		r.Handle(method, "/panic", kratosHandler(routes.Panic))
		r.Handle(method, "/status/{code}", kratosHandler(routes.Status))
		r.Handle(method, "/stream", kratosHandler(routes.Stream))
		r.Handle(method, "/fail", kratosHandler(routes.Fail))
		r.Handle(method, "/skip", kratosHandler(routes.OK))
	}
	return srv
}

// TestKratos_HTTP4_ReasonAndMetadata proves that a Kratos error records its reason and
// metadata on the open event, with the status the error encoder writes.
func TestKratos_HTTP4_ReasonAndMetadata(t *testing.T) {
	log, rec := wlogtest.New(t)
	srv := khttp.NewServer(
		khttp.Filter(wlogkratos.Filter(log)),
		khttp.Middleware(wlogkratos.Middleware()),
	)
	srv.Route("/").GET("/orders/{id}", serviceHandler(func(khttp.Context) error {
		return errors.New(http.StatusBadRequest, "ORDER_NOT_PAYABLE", "the order is not payable").
			WithMetadata(map[string]string{"order_id": "A-1"})
	}))

	srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/42", nil))

	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusBadRequest) {
		t.Errorf("http.status = %v, want 400", httpFields["status"])
	}
	if event["level"] != "warn" {
		t.Errorf("level = %v, want warn", event["level"])
	}
	if httpFields["reason"] != "ORDER_NOT_PAYABLE" {
		t.Errorf("http.reason = %v, want ORDER_NOT_PAYABLE", httpFields["reason"])
	}
	metadata, _ := httpFields["metadata"].(map[string]any)
	if metadata["order_id"] != "A-1" {
		t.Errorf("http.metadata = %v, want order_id A-1", httpFields["metadata"])
	}
}

// TestKratos_GRPC_UsesRPCInterceptors proves that a Kratos gRPC server records through the
// rpc-grpc interceptors, one event per call.
func TestKratos_GRPC_UsesRPCInterceptors(t *testing.T) {
	log, rec := wlogtest.New(t)
	lis := bufconn.Listen(1 << 20)
	srv := kgrpc.NewServer(
		kgrpc.Listener(lis),
		kgrpc.Options(grpc.ForceServerCodec(passCodec{})),
		kgrpc.UnaryInterceptor(wloggrpc.UnaryServerInterceptor(log)),
		kgrpc.StreamInterceptor(wloggrpc.StreamServerInterceptor(log)),
	)
	srv.RegisterService(&probeDesc, echoService{})
	// Serve starts the gRPC server of the Kratos server. Start cannot bind a bufconn
	// listener, because Kratos reads a port from its address.
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var reply []byte
	if err := conn.Invoke(context.Background(), "/probe.Probe/Echo", []byte("hi"), &reply, grpc.ForceCodec(passCodec{})); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	event := rec.Last()
	if event["kind"] != "rpc" {
		t.Errorf("kind = %v, want rpc", event["kind"])
	}
	rpcFields, _ := event["rpc"].(map[string]any)
	if rpcFields["system"] != "grpc" || rpcFields["service"] != "probe.Probe" || rpcFields["method"] != "Echo" {
		t.Errorf("rpc = %v, want system grpc, service probe.Probe, method Echo", rpcFields)
	}
}

// kratosHandler runs one net/http handler of the suite as a Kratos route handler, inside
// the Kratos middleware chain. The handler reads the event context, so it adds its own
// fields to the open event.
func kratosHandler(h http.HandlerFunc) khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		inner := ctx.Middleware(func(c context.Context, _ any) (any, error) {
			rec := httptest.NewRecorder()
			h(rec, ctx.Request().WithContext(c))
			for name, values := range rec.Header() {
				for _, value := range values {
					ctx.Response().Header().Add(name, value)
				}
			}
			ctx.Response().WriteHeader(rec.Code)
			_, _ = ctx.Response().Write(rec.Body.Bytes())
			return nil, nil
		})
		_, err := inner(ctx, nil)
		return err
	}
}

// serviceHandler runs one Kratos handler inside the middleware chain, the way a generated
// Kratos service does.
func serviceHandler(h khttp.HandlerFunc) khttp.HandlerFunc {
	return func(ctx khttp.Context) error {
		inner := ctx.Middleware(func(context.Context, any) (any, error) {
			return nil, h(ctx)
		})
		_, err := inner(ctx, nil)
		return err
	}
}

// probeService is the handler type of the gRPC probe service.
type probeService interface {
	Echo(context.Context, []byte) ([]byte, error)
}

// echoService answers the probe calls.
type echoService struct{}

// Echo answers one unary call with the request bytes.
func (echoService) Echo(_ context.Context, req []byte) ([]byte, error) { return req, nil }

// probeDesc describes the gRPC probe service.
var probeDesc = grpc.ServiceDesc{
	ServiceName: "probe.Probe",
	HandlerType: (*probeService)(nil),
	Methods: []grpc.MethodDesc{{
		MethodName: "Echo",
		Handler:    echoHandler,
	}},
	Metadata: "probe",
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

// passCodec carries message bytes through, so the test needs no generated code.
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
