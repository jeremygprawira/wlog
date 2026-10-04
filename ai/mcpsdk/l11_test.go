package wlogmcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	wlogmcp "github.com/jeremygprawira/wlog/ai/mcpsdk"
)

// TestMCPSDK_L11_JSONTextKeyIsMasked proves a denied key inside tool result JSON
// text is masked. The text is one string, so the key denylist cannot see it yet.
func TestMCPSDK_L11_JSONTextKeyIsMasked(t *testing.T) {
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"password":"hunter2"}`}}}, nil
	})
	rpc, _ := run(t, next, "tools/call", toolCallRequest("get_weather"), wlogmcp.WithContent())

	body, err := json.Marshal(rpc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(body), "hunter2") {
		t.Fatalf("password value reached the event: %s", body)
	}
}
