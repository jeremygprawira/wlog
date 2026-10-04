package wlogmcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	wlogmcp "github.com/jeremygprawira/wlog/ai/mcpsdk"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestMcpsdk_A24_HandlerErrorHidesArguments proves a handler error that quotes
// its arguments does not land in error.message unless content is on.
func TestMcpsdk_A24_HandlerErrorHidesArguments(t *testing.T) {
	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name:      "get_weather",
		Arguments: json.RawMessage(`{"city":"Jakarta"}`),
	}}
	next := mcp.MethodHandler(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, errors.New("bad city Jakarta")
	})
	log, rec := wlogtest.New(t)
	handler := wlogmcp.Middleware(log)(next)
	_, _ = handler(context.Background(), "tools/call", req)
	info, _ := rec.Last()["error"].(map[string]any)
	text, _ := info["message"].(string)
	if text == "" || strings.Contains(text, "Jakarta") {
		t.Fatalf("error.message = %q, want a message that hides the argument", text)
	}
}
