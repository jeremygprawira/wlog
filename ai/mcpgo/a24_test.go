package wlogmcpgo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	wlogmcpgo "github.com/jeremygprawira/wlog/ai/mcpgo"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestMcpgo_A24_HandlerErrorHidesArguments proves a handler error that quotes
// its arguments does not land in error.message unless content is on.
func TestMcpgo_A24_HandlerErrorHidesArguments(t *testing.T) {
	msg := &mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "get_weather",
		Arguments: map[string]any{"city": "Jakarta"},
	}}
	err := requestErrorLike{code: mcp.INTERNAL_ERROR, msg: "bad city Jakarta"}
	ctx := context.Background()

	log, rec := wlogtest.New(t)
	hooks := wlogmcpgo.Hooks(log)
	hooks.OnBeforeAny[0](ctx, "req-1", mcp.MethodToolsCall, msg)
	hooks.OnError[0](ctx, "req-1", mcp.MethodToolsCall, msg, err)
	info, _ := rec.Last()["error"].(map[string]any)
	text, _ := info["message"].(string)
	if text == "" || strings.Contains(text, "Jakarta") {
		t.Fatalf("error.message = %q, want a message that hides the argument", text)
	}

	log, rec = wlogtest.New(t)
	hooks = wlogmcpgo.Hooks(log, wlogmcpgo.WithContent())
	hooks.OnBeforeAny[0](ctx, "req-1", mcp.MethodToolsCall, msg)
	hooks.OnError[0](ctx, "req-1", mcp.MethodToolsCall, msg, err)
	info, _ = rec.Last()["error"].(map[string]any)
	text, _ = info["message"].(string)
	if !strings.Contains(text, "Jakarta") {
		t.Fatalf("error.message = %q, want the argument kept when content is on", text)
	}
}
