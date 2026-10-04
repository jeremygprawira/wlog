package wlogmcpgo_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/jeremygprawira/wlog"
)

// TestMcpgo_A18_RecordedToolCall loads a tools/call result the mcp-go server
// actually returned, and checks the event still says ok.
func TestMcpgo_A18_RecordedToolCall(t *testing.T) {
	body, err := os.ReadFile("testdata/tool_call.json")
	if err != nil {
		t.Fatal(err)
	}
	var result mcp.CallToolResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	rpc, level := fireRecorded(t, mcp.MethodToolsCall, toolCallMessage("get_weather"), &result, nil)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["result"] != "ok" {
		t.Fatalf("rpc.mcp.result = %v, want ok", mcpFields["result"])
	}
	if level != wlog.LevelInfo {
		t.Errorf("level = %q, want info", level)
	}
}

// TestMcpgo_A18_ClientCodesAreWarn loads the three client error codes and
// checks each one is a protocol error at warn. A server fault stays at error.
func TestMcpgo_A18_ClientCodesAreWarn(t *testing.T) {
	body, err := os.ReadFile("testdata/client_codes.json")
	if err != nil {
		t.Fatal(err)
	}
	var codes []mcp.JSONRPCErrorDetails
	if err := json.Unmarshal(body, &codes); err != nil {
		t.Fatal(err)
	}
	if len(codes) != 3 {
		t.Fatalf("codes = %d, want 3", len(codes))
	}
	for _, details := range codes {
		err := requestErrorLike{code: details.Code, msg: details.Message}
		rpc, level := fireRecorded(t, mcp.MethodToolsCall, toolCallMessage("get_weather"), nil, err)
		mcpFields, _ := rpc["mcp"].(map[string]any)
		if mcpFields["result"] != "protocol_error" || level != wlog.LevelWarn {
			t.Fatalf("code %d: result %v level %q, want protocol_error at warn", details.Code, mcpFields["result"], level)
		}
	}
}
