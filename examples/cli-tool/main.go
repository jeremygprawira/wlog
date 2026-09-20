// Command cli-tool is a small command line tool where one run is one wide event. It is
// the cli-tool recipe's example: a plain flag command, one unit of work, and the fields
// a reader needs to search a run.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// run runs one command and returns its exit code. One run is one wide event.
func run(ctx context.Context, logger *wlog.Logger, args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("orders", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	limit := flags.Int("limit", 10, "how many orders to read")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	ctx, handle := work.Start(ctx, logger, work.Unit{
		Kind: work.KindCommand,
		Fields: map[string]any{
			"path":  "orders list",
			"flags": []string{"limit"},
		},
	})
	wlog.Set(ctx, "orders_read", *limit)
	_, _ = fmt.Fprintf(stdout, "read %d orders\n", *limit)
	handle.Status("0", work.ClassOf(work.KindCommand, "0"))
	handle.End(nil)
	return 0
}

func main() {
	logger := wlog.New(wlog.WithService("cli-tool", "0.0.1", "local"))
	os.Exit(run(context.Background(), logger, os.Args[1:], os.Stdout))
}
