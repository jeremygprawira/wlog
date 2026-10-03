package wlogmcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog"
)

// TestMcpsdk_A18_TypedNilResultDoesNotPanic proves a protocol error whose
// result is a typed nil still ends the event. The result's fields are not read.
func TestMcpsdk_A18_TypedNilResultDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("typed nil result panicked: %v", r)
		}
	}()
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return (*mcp.CallToolResult)(nil), errors.New("boom")
	})
	rpc, level := run(t, next, "tools/call", toolCallRequest("get_weather"))
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "protocol_error" {
		t.Fatalf("rpc.mcp.result = %v, want protocol_error", mcpFields["result"])
	}
	if level != wlog.LevelError {
		t.Errorf("level = %q, want error", level)
	}
}

// TestMcpsdk_A18_NilInitializeParams proves a session that has not finished
// initialize still records the call. InitializeParams is nil on that session.
func TestMcpsdk_A18_NilInitializeParams(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil InitializeParams panicked: %v", r)
		}
	}()
	req := &mcp.CallToolRequest{
		Session: &mcp.ServerSession{},
		Params:  &mcp.CallToolParamsRaw{Name: "get_weather"},
	}
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "sunny"}}}, nil
	})
	rpc, _ := run(t, next, "tools/call", req)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["tool"] != "get_weather" || mcpFields["result"] != "ok" {
		t.Fatalf("rpc.mcp = %v, want tool get_weather and result ok", mcpFields)
	}
	if _, ok := mcpFields["protocol_version"]; ok {
		t.Fatalf("protocol_version = %v, want it unset", mcpFields["protocol_version"])
	}
}

// TestMcpsdk_A18_RecordedToolCall loads a tools/call result the go-sdk server
// actually returned, and checks the event still says ok.
func TestMcpsdk_A18_RecordedToolCall(t *testing.T) {
	body, err := os.ReadFile("testdata/tool_call.json")
	if err != nil {
		t.Fatal(err)
	}
	var result mcp.CallToolResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &result, nil
	})
	rpc, level := run(t, next, "tools/call", toolCallRequest("get_weather"))
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "ok" {
		t.Fatalf("rpc.mcp.result = %v, want ok", mcpFields["result"])
	}
	if level != wlog.LevelInfo {
		t.Errorf("level = %q, want info", level)
	}
}

// TestMcpsdk_A18_ClientCodesAreWarn loads the three client error codes and
// checks each one is a protocol error at warn.
func TestMcpsdk_A18_ClientCodesAreWarn(t *testing.T) {
	body, err := os.ReadFile("testdata/client_codes.json")
	if err != nil {
		t.Fatal(err)
	}
	var codes []jsonrpc.Error
	if err := json.Unmarshal(body, &codes); err != nil {
		t.Fatal(err)
	}
	if len(codes) != 3 {
		t.Fatalf("codes = %d, want 3", len(codes))
	}
	for _, details := range codes {
		wire := details
		next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
			return nil, &wire
		})
		rpc, level := run(t, next, "tools/call", toolCallRequest("get_weather"))
		mcpFields, _ := rpc["mcp"].(map[string]any)
		if mcpFields["result"] != "protocol_error" || level != wlog.LevelWarn {
			t.Fatalf("code %d: result %v level %q, want protocol_error at warn", details.Code, mcpFields["result"], level)
		}
	}
}
