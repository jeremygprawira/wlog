// This file calls one RPC over bufconn and compares the event with the recipe's
// hand-written golden. The schema tool validates the golden against schema/event.v1.json.
package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremygprawira/wlog/internal/conformance"
)

// TestGRPCService_GoldenEvent proves that one health check gives the event the recipe
// documents.
func TestGRPCService_GoldenEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	server := newServer(rec.Logger())
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"x-request-id", "recipe-1")
	response, err := grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want SERVING", response.GetStatus())
	}

	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	want := golden(t)
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(want), got); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}
}

// golden reads the recipe's hand-written event.
func golden(t *testing.T) map[string]any {
	t.Helper()
	body, err := os.ReadFile("testdata/event.json")
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	event := map[string]any{}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("parse the golden: %v", err)
	}
	return event
}
