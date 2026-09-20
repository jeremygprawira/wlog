// This file starts the MCP server over in-memory transports and calls every tool with the
// go-sdk client, so the tool set and the answers are proven end to end.
package mcpserver_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/mcpserver"
)

// TestMCP_Tools proves that every tool answers the go-sdk client over the protocol.
func TestMCP_Tools(t *testing.T) {
	root := t.TempDir()
	events := filepath.Join(root, "events.ndjson")
	body := `{"timestamp":"2026-09-16T08:00:00Z","level":"info","kind":"request","operation":"GET /orders/{id}","duration_ms":12,"trace":{"trace_id":"t-1","request_id":"r-1"},"http":{"status":200}}` + "\n" +
		`{"timestamp":"2026-09-16T08:00:01Z","level":"error","kind":"request","operation":"POST /orders/{id}","duration_ms":840,"trace":{"trace_id":"t-2","request_id":"r-2"},"http":{"status":502},"error":{"code":"PAYMENT_DECLINED"}}` + "\n"
	if err := os.WriteFile(events, []byte(body), 0o644); err != nil {
		t.Fatalf("write the events: %v", err)
	}
	mapFile := filepath.Join(root, "wlog.map.json")
	if err := os.WriteFile(mapFile, []byte(`{"version":2,"handlers":[{"function":"handleOrder","file":"main.go","score":80}]}`), 0o644); err != nil {
		t.Fatalf("write the map: %v", err)
	}

	server := mcpserver.Server([]string{root})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "wlog-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	calls := map[string]map[string]any{
		"events_query":         {"source": events, "level": []string{"error"}, "limit": 10},
		"events_by_request_id": {"source": events, "request_id": "r-2"},
		"events_by_trace_id":   {"source": events, "trace_id": "t-1"},
		"map_entry":            {"name": "handleOrder"},
		"explain":              {"id": "WLOG_NO_EVENT"},
		"redact_check":         {"key": "password"},
		"schema_event":         {},
	}
	for name, arguments := range calls {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if result.IsError {
			t.Errorf("%s: the tool reported an error: %v", name, result.Content)
			continue
		}
		if result.StructuredContent == nil {
			t.Errorf("%s: no structured content", name)
		}
	}
}

// TestMCP_EventsQueryFilters proves that the query tool applies the filters and the
// limit.
func TestMCP_EventsQueryFilters(t *testing.T) {
	root := t.TempDir()
	events := filepath.Join(root, "events.ndjson")
	body := `{"level":"info","operation":"GET /a"}` + "\n" +
		`{"level":"error","operation":"POST /b"}` + "\n" +
		`{"level":"error","operation":"POST /c"}` + "\n"
	if err := os.WriteFile(events, []byte(body), 0o644); err != nil {
		t.Fatalf("write the events: %v", err)
	}

	server := mcpserver.Server([]string{root})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "wlog-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "events_query",
		Arguments: map[string]any{"source": events, "level": []string{"error"}, "limit": 1},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	answer, _ := result.StructuredContent.(map[string]any)
	if count, _ := answer["count"].(float64); count != 1 {
		t.Errorf("count = %v, want 1", answer["count"])
	}
	eventsOut, _ := answer["events"].([]any)
	if len(eventsOut) != 1 {
		t.Fatalf("events = %v, want the newest error only", answer["events"])
	}
	newest, _ := eventsOut[0].(map[string]any)
	if newest["operation"] != "POST /c" {
		t.Errorf("operation = %v, want the newest error", newest["operation"])
	}
}

// TestMCP_RootGuard proves that a path outside every root is refused.
func TestMCP_RootGuard(t *testing.T) {
	root := t.TempDir()
	server := mcpserver.Server([]string{root})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "wlog-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "events_query",
		Arguments: map[string]any{"source": "/etc/hosts"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !result.IsError {
		t.Error("a path outside the root was accepted")
	}
}
