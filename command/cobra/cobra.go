// This file holds the command runner, the exit code rule, and the mapping from one run onto
// one unit of work.
package wlogcobra

import (
	"context"
	"errors"
	"flag"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// flushTimeout is the budget of the flush that runs after every command.
const flushTimeout = 2 * time.Second

// Execute runs root with ctx, gives the run one command event, flushes the drains, and returns
// the code for os.Exit. A usage fault gives 2, and any other failure gives 1. The event carries
// the command path, the names of the flags that were set, and the exit code. A nil Logger
// means wlog.Default.
func Execute(ctx context.Context, log *wlog.Logger, root *cobra.Command) int {
	// The flush runs even when the run panics, because os.Exit after the panic runs no
	// defer of its own.
	defer flush(log)
	code := 0
	_ = work.Run(ctx, log, work.Unit{Kind: work.KindCommand}, func(ctx context.Context) error {
		cmd, err := root.ExecuteContextC(ctx)
		code = exitCode(err)
		record(ctx, cmd, code)
		return err
	})
	return code
}

// process runs one unit of work through the event path of this adapter, with a recovered panic
// as an error, so a test continues after the panic scenario.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// record writes the path, the flags, and the exit code of one run, and the level of a usage
// fault.
func record(ctx context.Context, cmd *cobra.Command, code int) {
	if cmd != nil {
		if path := cmd.CommandPath(); path != "" {
			wlog.Set(ctx, "operation", path)
			wlog.SetGroup(ctx, "cli", "path", path)
		}
		if names := flagNames(cmd); len(names) > 0 {
			wlog.SetGroup(ctx, "cli", "flags", names)
		}
	}
	wlog.SetGroup(ctx, "cli", "exit_code", code)
	if class := work.ClassOf(work.KindCommand, strconv.Itoa(code)); class == work.StatusClientError {
		wlog.SetLevel(ctx, wlog.LevelWarn)
	}
}

// exitCode returns the process code of one run. A usage fault gives 2, and any other failure
// gives 1. Cobra names no usage error type, so the message prefixes of its own faults decide.
func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case usageFault(err):
		return 2
	}
	return 1
}

// usagePrefixes holds the message prefixes that cobra and pflag write for a fault in the
// command line.
var usagePrefixes = []string{
	"unknown command", "unknown flag", "unknown shorthand flag", "flag needs an argument",
	"invalid argument", "required flag",
}

// usageFault reports whether one error names a fault in the command line.
func usageFault(err error) bool {
	text := err.Error()
	for _, prefix := range usagePrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// flagNames returns the names of the flags that were set, never their values.
func flagNames(cmd *cobra.Command) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, set := range []*pflag.FlagSet{cmd.Flags(), cmd.InheritedFlags()} {
		set.Visit(func(f *pflag.Flag) {
			if !seen[f.Name] {
				seen[f.Name] = true
				names = append(names, f.Name)
			}
		})
	}
	sort.Strings(names)
	return names
}

// flush sends the pending events of log on its own deadline, because the run context may be
// spent when the command ends.
func flush(log *wlog.Logger) {
	if log == nil {
		log = wlog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	_ = log.Flush(ctx)
}
