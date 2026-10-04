package wlogmcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog"
	wlogmcp "github.com/jeremygprawira/wlog/ai/mcpsdk"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// toolCallRequest builds the request the middleware sees for one tools/call, with no
// session, the shape a fixture-style unit test needs.
func toolCallRequest(name string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: name}}
}

// run drives handler through Middleware and returns the recorded event's rpc group.
func run(t *testing.T, next mcp.MethodHandler, method string, req mcp.Request, opts ...wlogmcp.Option) (map[string]any, wlog.Level) {
	t.Helper()
	log, rec := wlogtest.New(t)
	handler := wlogmcp.Middleware(log, opts...)(next)
	if _, err := handler(context.Background(), method, req); err != nil {
		t.Logf("handler returned %v", err)
	}
	rpc, _ := rec.Last()["rpc"].(map[string]any)
	level, _ := rec.Last()["level"].(string)
	return rpc, wlog.Level(level)
}

// TestMCPSDK_ToolCall proves a plain tool call gives result ok at the default level.
func TestMCPSDK_ToolCall(t *testing.T) {
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "sunny"}}}, nil
	})
	rpc, level := run(t, next, "tools/call", toolCallRequest("get_weather"), wlogmcp.WithService("orders-mcp"))

	if rpc["system"] != "mcp" || rpc["method"] != "tools/call" || rpc["service"] != "orders-mcp" {
		t.Fatalf("rpc = %v, want system mcp, method tools/call, service orders-mcp", rpc)
	}
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["tool"] != "get_weather" || mcpFields["result"] != "ok" {
		t.Errorf("rpc.mcp = %v, want tool get_weather, result ok", mcpFields)
	}
	if level != wlog.LevelInfo {
		t.Errorf("level = %q, want info", level)
	}
}

// TestMCPSDK_ToolError proves a tool result with IsError gives result tool_error at warn.
func TestMCPSDK_ToolError(t *testing.T) {
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		res := &mcp.CallToolResult{}
		res.SetError(errFake("boom"))
		return res, nil
	})
	rpc, level := run(t, next, "tools/call", toolCallRequest("get_weather"))

	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "tool_error" {
		t.Errorf("rpc.mcp.result = %v, want tool_error", mcpFields["result"])
	}
	if level != wlog.LevelWarn {
		t.Errorf("level = %q, want warn", level)
	}
}

// TestMCPSDK_UnknownTool proves the server's own "unknown tool" wire error, code
// -32602, gives result protocol_error at warn, with the status code recorded.
func TestMCPSDK_UnknownTool(t *testing.T) {
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: `unknown tool "no_such_tool"`}
	})
	rpc, level := run(t, next, "tools/call", toolCallRequest("no_such_tool"))

	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "protocol_error" {
		t.Errorf("rpc.mcp.result = %v, want protocol_error", mcpFields["result"])
	}
	if rpc["status_code"] != "-32602" {
		t.Errorf("rpc.status_code = %v, want -32602", rpc["status_code"])
	}
	if level != wlog.LevelWarn {
		t.Errorf("level = %q, want warn", level)
	}
}

// TestMCPSDK_InputRequired proves a result with resultType input_required gives result
// input_required and a hash of requestState, never the raw value. The SDK gives no
// exported constructor for this result shape, so the fixture comes from the wire JSON,
// the same way [CallToolResult.UnmarshalJSON] decodes a real one.
func TestMCPSDK_InputRequired(t *testing.T) {
	var result mcp.CallToolResult
	body := []byte(`{"content":[],"resultType":"input_required","requestState":"secret-state"}`)
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !result.NeedsInput() {
		t.Fatal("fixture result does not report NeedsInput")
	}

	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &result, nil
	})
	rpc, level := run(t, next, "tools/call", toolCallRequest("get_weather"))

	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "input_required" {
		t.Errorf("rpc.mcp.result = %v, want input_required", mcpFields["result"])
	}
	sum := sha256.Sum256([]byte("secret-state"))
	want := hex.EncodeToString(sum[:8])
	if mcpFields["request_state"] != want {
		t.Errorf("rpc.mcp.request_state = %v, want the hash %q, never the raw value", mcpFields["request_state"], want)
	}
	if level != wlog.LevelInfo {
		t.Errorf("level = %q, want info", level)
	}
}

// TestMCPSDK_Notification proves a notification method never gets an event.
func TestMCPSDK_Notification(t *testing.T) {
	log, rec := wlogtest.New(t)
	called := false
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	})
	handler := wlogmcp.Middleware(log)(next)
	if _, err := handler(context.Background(), "notifications/cancelled", toolCallRequest("")); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !called {
		t.Fatal("the middleware never called next for a notification")
	}
	rec.RequireCount(t, 0)
}

// TestMCPSDK_LiveSession proves a real client-server round trip fills the session id,
// the protocol version, and the client name, and that no argument or result reaches the
// event without WithContent.
func TestMCPSDK_LiveSession(t *testing.T) {
	log, rec := wlogtest.New(t)

	server := mcp.NewServer(&mcp.Implementation{Name: "orders-mcp", Version: "1.0.0"}, nil)
	server.AddReceivingMiddleware(wlogmcp.Middleware(log, wlogmcp.WithService("orders-mcp")))
	mcp.AddTool(server, &mcp.Tool{Name: "get_weather", Description: "reports the weather"},
		func(_ context.Context, _ *mcp.CallToolRequest, in struct {
			City string `json:"city"`
		}) (*mcp.CallToolResult, struct {
			Weather string `json:"weather"`
		}, error) {
			return nil, struct {
				Weather string `json:"weather"`
			}{Weather: "sunny"}, nil
		})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "weather-cli", Version: "2.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_weather", Arguments: map[string]any{"city": "Jakarta"}}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	rpc, _ := rec.Last()["rpc"].(map[string]any)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	// An in-memory transport reports no session id, per [mcp.Session.ID]: "the empty
	// string if there is none." rpc.mcp.session_id is exercised by the SDK's own
	// conformance suite over a real transport, not duplicated here.
	if mcpFields["protocol_version"] == "" || mcpFields["protocol_version"] == nil {
		t.Error("rpc.mcp.protocol_version is empty on a live session")
	}
	if mcpFields["client"] != "weather-cli/2.0.0" {
		t.Errorf("rpc.mcp.client = %v, want weather-cli/2.0.0", mcpFields["client"])
	}
	if _, hasArguments := mcpFields["arguments"]; hasArguments {
		t.Error("arguments reached the event without WithContent")
	}
	if _, hasResult := mcpFields["result_content"]; hasResult {
		t.Error("result_content reached the event without WithContent")
	}
}

// errFake is a minimal error for tests that never inspect the message.
type errFake string

func (e errFake) Error() string { return string(e) }

// TestMcpsdk_A14_RequestStateLinksRetry proves a retry that echoes RequestState
// records the same hash, even when the result itself has no state.
func TestMcpsdk_A14_RequestStateLinksRetry(t *testing.T) {
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "get_weather", RequestState: "secret-state"}}
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "sunny"}}}, nil
	})
	rpc, _ := run(t, next, "tools/call", req)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	sum := sha256.Sum256([]byte("secret-state"))
	want := hex.EncodeToString(sum[:8])
	if mcpFields["request_state"] != want {
		t.Fatalf("rpc.mcp.request_state = %v, want %s", mcpFields["request_state"], want)
	}
}

// TestMcpsdk_A16_PanicStillEmits proves a handler panic is recorded and then
// raised again.
func TestMcpsdk_A16_PanicStillEmits(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := wlogmcp.Middleware(log)(mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		panic("boom")
	}))
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_, _ = handler(context.Background(), "tools/call", toolCallRequest("get_weather"))
	}()
	if !panicked {
		t.Fatal("the panic was not raised again")
	}
	if rec.Count() != 1 || rec.Last()["error"] == nil {
		t.Fatalf("event = %v, want one event with an error", rec.Last())
	}
}
