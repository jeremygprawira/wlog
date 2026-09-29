// This file calls the one tool through a real client-server round trip over an in-memory
// transport, and compares the event with the recipe's hand-written golden. The schema tool
// validates the golden against schema/event.v1.json.
package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog/internal/conformance"
)

// protocolVersion pins the client to the SEP-2575 protocol, so the negotiated version in
// the golden never drifts with whatever version the SDK defaults to next.
const protocolVersion = "2026-07-28"

// TestMCPServer_GoldenEvent proves that one tool call gives the event the recipe
// documents.
func TestMCPServer_GoldenEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	server := newServer(rec.Logger())

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "weather-cli", Version: "2.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_weather", Arguments: map[string]any{"city": "Jakarta"}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool returned an error result: %v", result.Content)
	}

	// The handshake gives its own event too: one per MCP request, initialize included.
	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (initialize, then tools/call)", len(events))
	}
	want := golden(t)
	got := conformance.Normalize(events[1])
	if diff := conformance.Diff(conformance.Normalize(want), got); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}
}

// TestMCPServer_UnknownTool proves that calling a tool the server never registered gives
// a warn-level event, of result protocol_error.
func TestMCPServer_UnknownTool(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	server := newServer(rec.Logger())

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "weather-cli", Version: "2.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, &mcp.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "no_such_tool"}); err == nil {
		t.Fatal("CallTool returned no error for an unknown tool")
	}

	last := rec.Last()
	if last["level"] != "warn" {
		t.Errorf("level = %v, want warn", last["level"])
	}
	rpc, _ := last["rpc"].(map[string]any)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "protocol_error" {
		t.Errorf("rpc.mcp.result = %v, want protocol_error", mcpFields["result"])
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
