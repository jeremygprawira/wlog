// Command mcp-server runs an MCP server with one tool, where wlogmcp gives every request
// one wide event. It is the mcp-server recipe's example: one event per tool call, of kind
// rpc with rpc.system mcp.
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog"
	wlogmcp "github.com/jeremygprawira/wlog/ai/mcpsdk"
)

// weatherInput is the input of get_weather.
type weatherInput struct {
	City string `json:"city" jsonschema:"the city to report"`
}

// weatherOutput is the output of get_weather.
type weatherOutput struct {
	Weather string `json:"weather"`
}

// newServer builds the MCP server: one tool, and wlogmcp on every request.
func newServer(logger *wlog.Logger) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "weather-mcp", Version: "0.0.1"}, nil)
	server.AddReceivingMiddleware(wlogmcp.Middleware(logger, wlogmcp.WithService("weather-mcp")))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_weather",
		Description: "Reports the weather of one city.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in weatherInput) (*mcp.CallToolResult, weatherOutput, error) {
		return nil, weatherOutput{Weather: "sunny"}, nil
	})
	return server
}

func main() {
	logger := wlog.New(wlog.WithService("weather-mcp", "0.0.1", "local"))
	server := newServer(logger)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
	// The process ends here, so the pending events are sent now.
	_ = logger.Flush(context.Background())
}
