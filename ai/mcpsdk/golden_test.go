package wlogmcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	wlogmcp "github.com/jeremygprawira/wlog/ai/mcpsdk"
	"github.com/jeremygprawira/wlog/internal/mcpfixture"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestMcpsdk_A11_MatchesSharedGolden proves the four MCP fixtures match the shared
// golden. error.message, error.type, and error.cause are not part of that golden.
func TestMcpsdk_A11_MatchesSharedGolden(t *testing.T) {
	var input mcp.CallToolResult
	body := []byte(`{"content":[],"resultType":"input_required","requestState":"secret-state"}`)
	if err := json.Unmarshal(body, &input); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		next mcp.MethodHandler
		tool string
	}{
		{
			name: mcpfixture.ToolCall,
			tool: "get_weather",
			next: func(context.Context, string, mcp.Request) (mcp.Result, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "sunny"}}}, nil
			},
		},
		{
			name: mcpfixture.ToolError,
			tool: "get_weather",
			next: func(context.Context, string, mcp.Request) (mcp.Result, error) {
				res := &mcp.CallToolResult{}
				res.SetError(errFake("boom"))
				return res, nil
			},
		},
		{
			name: mcpfixture.UnknownTool,
			tool: "no_such_tool",
			next: func(context.Context, string, mcp.Request) (mcp.Result, error) {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: `unknown tool "no_such_tool"`}
			},
		},
		{
			name: mcpfixture.InputRequired,
			tool: "get_weather",
			next: func(context.Context, string, mcp.Request) (mcp.Result, error) {
				return &input, nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			handler := wlogmcp.Middleware(log, wlogmcp.WithService("orders-mcp"))(tc.next)
			if _, err := handler(context.Background(), "tools/call", toolCallRequest(tc.tool)); err != nil {
				t.Logf("handler returned %v", err)
			}
			if err := mcpfixture.Equal(rec.Last(), tc.name); err != nil {
				t.Fatal(err)
			}
		})
	}
}
