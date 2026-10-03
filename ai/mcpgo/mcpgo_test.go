package wlogmcpgo_test

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/jeremygprawira/wlog"
	wlogmcpgo "github.com/jeremygprawira/wlog/ai/mcpgo"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// requestErrorLike adapts a bare JSON-RPC code to the ToJSONRPCError shape every error
// mcp-go hands to OnError carries, without depending on the unexported type that really
// produces it.
type requestErrorLike struct {
	code int
	msg  string
}

func (e requestErrorLike) Error() string { return e.msg }
func (e requestErrorLike) ToJSONRPCError() mcp.JSONRPCError {
	return mcp.JSONRPCError{Error: mcp.JSONRPCErrorDetails{Code: e.code, Message: e.msg}}
}

// fireRecorded drives one before/after hook pair against a fresh wlogtest recorder, and
// returns the recorded event's rpc group. Hooks registers exactly one handler each of
// AddBeforeAny, AddOnSuccess, and AddOnError, so calling index 0 reaches it directly.
func fireRecorded(t *testing.T, method mcp.MCPMethod, message any, result any, err error, opts ...wlogmcpgo.Option) (map[string]any, wlog.Level) {
	t.Helper()
	log, rec := wlogtest.New(t)
	hooks := wlogmcpgo.Hooks(log, opts...)

	ctx := context.Background()
	hooks.OnBeforeAny[0](ctx, "req-1", method, message)
	if err != nil {
		hooks.OnError[0](ctx, "req-1", method, message, err)
	} else {
		hooks.OnSuccess[0](ctx, "req-1", method, message, result)
	}

	rpc, _ := rec.Last()["rpc"].(map[string]any)
	level, _ := rec.Last()["level"].(string)
	return rpc, wlog.Level(level)
}

func toolCallMessage(name string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name}}
}

// TestMCPGo_ToolCall proves a plain tool call gives result ok at the default level.
func TestMCPGo_ToolCall(t *testing.T) {
	result := &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("sunny")}}
	rpc, level := fireRecorded(t, mcp.MethodToolsCall, toolCallMessage("get_weather"), result, nil, wlogmcpgo.WithService("orders-mcp"))

	if rpc["system"] != "mcp" || rpc["method"] != string(mcp.MethodToolsCall) || rpc["service"] != "orders-mcp" {
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

// TestMCPGo_ToolError proves a tool result with IsError gives result tool_error at warn.
func TestMCPGo_ToolError(t *testing.T) {
	result := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{mcp.NewTextContent("boom")}}
	rpc, level := fireRecorded(t, mcp.MethodToolsCall, toolCallMessage("get_weather"), result, nil)

	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "tool_error" {
		t.Errorf("rpc.mcp.result = %v, want tool_error", mcpFields["result"])
	}
	if level != wlog.LevelWarn {
		t.Errorf("level = %q, want warn", level)
	}
}

// TestMCPGo_UnknownTool proves the server's own "unknown tool" wire error, code
// INVALID_PARAMS, gives result protocol_error at warn, with the status code recorded.
func TestMCPGo_UnknownTool(t *testing.T) {
	err := requestErrorLike{code: mcp.INVALID_PARAMS, msg: `tool "no_such_tool" not found`}
	rpc, level := fireRecorded(t, mcp.MethodToolsCall, toolCallMessage("no_such_tool"), nil, err)

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

// TestMCPGo_InputRequired proves a result whose ResultType is input_required gives result
// input_required and a hash of requestState, never the raw value.
func TestMCPGo_InputRequired(t *testing.T) {
	result := &mcp.CallToolResult{
		Result:               mcp.Result{ResultType: mcp.ResultTypeInputRequired},
		MultiRoundTripResult: mcp.MultiRoundTripResult{RequestState: "secret-state"},
	}
	if !result.NeedsInput() {
		t.Fatal("fixture result does not report NeedsInput")
	}
	rpc, level := fireRecorded(t, mcp.MethodToolsCall, toolCallMessage("get_weather"), result, nil)

	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "input_required" {
		t.Errorf("rpc.mcp.result = %v, want input_required", mcpFields["result"])
	}
	if mcpFields["request_state"] == "secret-state" {
		t.Error("rpc.mcp.request_state stored the raw value, want a hash")
	}
	if mcpFields["request_state"] == nil || mcpFields["request_state"] == "" {
		t.Error("rpc.mcp.request_state is empty")
	}
	if level != wlog.LevelInfo {
		t.Errorf("level = %q, want info", level)
	}
}

// TestMCPGo_Notification proves a notification method never gets an event.
func TestMCPGo_Notification(t *testing.T) {
	log, rec := wlogtest.New(t)
	hooks := wlogmcpgo.Hooks(log)
	hooks.OnBeforeAny[0](context.Background(), nil, mcp.MethodNotificationCancelled, nil)
	rec.RequireCount(t, 0)
}

// TestMCPGo_LiveSession proves a real client-server round trip fills the client name, and
// that no argument or result reaches the event without WithContent.
func TestMCPGo_LiveSession(t *testing.T) {
	log, rec := wlogtest.New(t)

	mcpServer := server.NewMCPServer("orders-mcp", "1.0.0", server.WithHooks(wlogmcpgo.Hooks(log, wlogmcpgo.WithService("orders-mcp"))))
	mcpServer.AddTool(mcp.NewTool("get_weather", mcp.WithDescription("reports the weather")),
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("sunny")}}, nil
		})

	mcpClient, err := client.NewInProcessClient(mcpServer)
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	defer func() { _ = mcpClient.Close() }()

	ctx := context.Background()
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	initReq := mcp.InitializeRequest{}
	initReq.Params.ClientInfo = mcp.Implementation{Name: "weather-cli", Version: "2.0.0"}
	if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "get_weather"
	callReq.Params.Arguments = map[string]any{"city": "Jakarta"}
	if _, err := mcpClient.CallTool(ctx, callReq); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	rpc, _ := rec.Last()["rpc"].(map[string]any)
	mcpFields, _ := rpc["mcp"].(map[string]any)
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

// TestMcpgo_A1_ArrayIdDoesNotPanic proves a JSON-RPC id that is an array or an
// object does not panic the before hook, and the after hook still ends the event.
func TestMcpgo_A1_ArrayIdDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("array or object id panicked the hook: %v", r)
		}
	}()
	log, rec := wlogtest.New(t)
	hooks := wlogmcpgo.Hooks(log)
	ctx := context.Background()
	message := toolCallMessage("get_weather")
	result := &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("sunny")}}

	for _, id := range []any{[]any{"batch", 1}, map[string]any{"n": 1}} {
		hooks.OnBeforeAny[0](ctx, id, mcp.MethodToolsCall, message)
		hooks.OnSuccess[0](ctx, id, mcp.MethodToolsCall, message, result)
	}
	rec.RequireCount(t, 2)
}

// TestMcpgo_A2_NilResultDoesNotPanic proves a tool handler that returns nil, nil
// still ends the event. The SDK answers that call with a null result.
func TestMcpgo_A2_NilResultDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil result panicked the after hook: %v", r)
		}
	}()
	var result *mcp.CallToolResult
	rpc, level := fireRecorded(t, mcp.MethodToolsCall, toolCallMessage("get_weather"), result, nil)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "ok" {
		t.Fatalf("rpc.mcp.result = %v, want ok", mcpFields["result"])
	}
	if level != wlog.LevelInfo {
		t.Errorf("level = %q, want info", level)
	}
}
