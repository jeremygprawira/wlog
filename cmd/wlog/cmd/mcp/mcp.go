// Command mcp is wlog mcp: a read-only MCP server over stdio. Every tool answers a
// question about wlog events, and a file tool reads only under the roots given here.
package mcp

import (
	"context"
	"flag"
	"fmt"
	"io"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/mcpserver"
)

// rootList collects the repeatable root flags.
type rootList []string

// String returns the roots.
func (r *rootList) String() string { return fmt.Sprint([]string(*r)) }

// Set adds one root.
func (r *rootList) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// Run runs the server over stdio.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var roots rootList
	flags.Var(&roots, "root", "a directory the tools may read, repeatable, default .")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if len(roots) == 0 {
		roots = rootList{"."}
	}

	server := mcpserver.Server(roots)
	if err := server.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		_, _ = fmt.Fprintf(stderr, "wlog mcp: %v\n", err)
		return 2
	}
	_ = stdout
	return 0
}
