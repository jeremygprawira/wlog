package wlogmcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog"
	wlogmcp "github.com/jeremygprawira/wlog/ai/mcpsdk"
	"github.com/jeremygprawira/wlog/redact"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestMcpsdk_A13_SessionHash proves a streamable session records a short hash
// of its id under rpc.mcp.session.
func TestMcpsdk_A13_SessionHash(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithRedactor(redact.Disabled()))
	impl := &mcp.Implementation{Name: "orders-mcp", Version: "1.0.0"}
	srv := mcp.NewServer(impl, nil)
	srv.AddReceivingMiddleware(wlogmcp.Middleware(log))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_weather"}, func(_ context.Context, req *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: req.Session.ID()}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	ctx := context.Background()
	client := mcp.NewClient(impl, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_weather"})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := res.Content[0].(*mcp.TextContent)
	if text == nil || text.Text == "" {
		t.Fatal("tool did not echo a session id")
	}
	sum := sha256.Sum256([]byte(text.Text))
	want := hex.EncodeToString(sum[:8])
	rpc, _ := rec.Last()["rpc"].(map[string]any)
	mcpFields, _ := rpc["mcp"].(map[string]any)
	if mcpFields["session"] != want {
		t.Fatalf("rpc.mcp.session = %v, want hash %s of %q", mcpFields["session"], want, text.Text)
	}
}
