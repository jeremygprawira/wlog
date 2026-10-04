package wlogmcpgo_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/jeremygprawira/wlog"
	wlogmcpgo "github.com/jeremygprawira/wlog/ai/mcpgo"
	"github.com/jeremygprawira/wlog/redact"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestMcpgo_A13_SessionHash proves a later call records a short hash of the
// session id under rpc.mcp.session.
func TestMcpgo_A13_SessionHash(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithRedactor(redact.Disabled()))
	mcpServer := server.NewMCPServer("orders-mcp", "1.0.0", server.WithHooks(wlogmcpgo.Hooks(log)))
	mcpServer.AddTool(mcp.NewTool("get_weather"), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
	initReq.Params.ProtocolVersion = "2025-03-26"
	if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
		t.Fatal(err)
	}
	call := mcp.CallToolRequest{}
	call.Params.Name = "get_weather"
	if _, err := mcpClient.CallTool(ctx, call); err != nil {
		t.Fatal(err)
	}
	rpc, _ := rec.Last()["rpc"].(map[string]any)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	id, _ := mcpFields["session_id"].(string)
	if id == "" {
		t.Fatal("session_id is empty")
	}
	sum := sha256.Sum256([]byte(id))
	want := hex.EncodeToString(sum[:8])
	if mcpFields["session"] != want {
		t.Fatalf("rpc.mcp.session = %v, want hash %s", mcpFields["session"], want)
	}
}
