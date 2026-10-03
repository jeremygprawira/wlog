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

// TestMcpgo_A12_ToolHandlerSeesEvent proves a tool handler can wlog.Set on the
// request event. The SDK context alone holds no event.
func TestMcpgo_A12_ToolHandlerSeesEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	hooks, mw := wlogmcpgo.Open(log, wlogmcpgo.WithService("orders-mcp"))
	mcpServer := server.NewMCPServer("orders-mcp", "1.0.0", server.WithHooks(hooks), server.WithToolHandlerMiddleware(mw))
	mcpServer.AddTool(mcp.NewTool("get_weather"), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		wlog.Set(ctx, "city", "Jakarta")
		return &mcp.CallToolResult{Content: []mcp.Content{mcp.NewTextContent("sunny")}}, nil
	})
	mcpClient, err := client.NewInProcessClient(mcpServer)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mcpClient.Close() }()
	ctx := context.Background()
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatal(err)
	}
	initReq := mcp.InitializeRequest{}
	initReq.Params.ClientInfo = mcp.Implementation{Name: "weather-cli", Version: "1.0.0"}
	if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
		t.Fatal(err)
	}
	call := mcp.CallToolRequest{}
	call.Params.Name = "get_weather"
	if _, err := mcpClient.CallTool(ctx, call); err != nil {
		t.Fatal(err)
	}
	if rec.Last()["city"] != "Jakarta" {
		t.Fatalf("city = %v, want Jakarta on the request event", rec.Last()["city"])
	}
}
