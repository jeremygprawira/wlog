package wlogmcpgo_test

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	wlogmcpgo "github.com/jeremygprawira/wlog/ai/mcpgo"
	"github.com/jeremygprawira/wlog/internal/mcpfixture"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestMcpgo_A11_MatchesSharedGolden proves the four MCP fixtures match the shared
// golden. error.message, error.type, and error.cause are not part of that golden.
func TestMcpgo_A11_MatchesSharedGolden(t *testing.T) {
	cases := []struct {
		name   string
		result any
		err    error
	}{
		{name: mcpfixture.ToolCall, result: &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("sunny")}}},
		{name: mcpfixture.ToolError, result: &mcp.CallToolResult{IsError: true, Content: []mcp.Content{mcp.NewTextContent("boom")}}},
		{name: mcpfixture.UnknownTool, err: requestErrorLike{code: mcp.INVALID_PARAMS, msg: `unknown tool "no_such_tool"`}},
		{name: mcpfixture.InputRequired, result: &mcp.CallToolResult{
			Result:               mcp.Result{ResultType: mcp.ResultTypeInputRequired},
			MultiRoundTripResult: mcp.MultiRoundTripResult{RequestState: "secret-state"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, rec := wlogtest.New(t)
			hooks := wlogmcpgo.Hooks(log, wlogmcpgo.WithService("orders-mcp"))
			ctx := context.Background()
			tool := "get_weather"
			if tc.name == mcpfixture.UnknownTool {
				tool = "no_such_tool"
			}
			msg := toolCallMessage(tool)
			hooks.OnBeforeAny[0](ctx, "req-1", mcp.MethodToolsCall, msg)
			if tc.err != nil {
				hooks.OnError[0](ctx, "req-1", mcp.MethodToolsCall, msg, tc.err)
			} else {
				hooks.OnSuccess[0](ctx, "req-1", mcp.MethodToolsCall, msg, tc.result)
			}
			if err := mcpfixture.Equal(rec.Last(), tc.name); err != nil {
				t.Fatal(err)
			}
		})
	}
}
