// This file holds the command runner, the exit code rule, and the mapping from one run onto
// one unit of work.
package wlogkong

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"time"

	"github.com/alecthomas/kong"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// flushTimeout is the budget of the flush that runs after every command.
const flushTimeout = 2 * time.Second

// Run parses grammar with args in the form kong.Parse takes, which is the argument list after
// the program name, runs the selected command, gives the run one command event,
// flushes the drains, and returns the code for os.Exit. A fault in the command line gives 80,
// an ExitCoder error names its own code, and any other failure gives 1. The event carries the
// command path, the names of the flags that were set, and the exit code. A nil Logger means
// wlog.Default.
func Run(ctx context.Context, log *wlog.Logger, grammar any, args []string, opts ...kong.Option) int {
	code := 0
	ctx, handle := work.Start(ctx, log, work.Unit{Kind: work.KindCommand})
	ended := false
	end := func(parsed *kong.Context, exitCode int, err error) {
		if ended {
			return
		}
		ended = true
		record(ctx, parsed, exitCode)
		if isUsage(err) {
			wlog.SetLevel(ctx, wlog.LevelWarn)
		}
		handle.End(err)
		flush(log)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			wlog.Error(ctx, &panicError{value: recovered, stack: string(debug.Stack())})
			end(nil, 1, nil)
			panic(recovered)
		}
	}()

	parser, err := kong.New(grammar, withExit(opts, func(exitCode int) {
		// kong exits inside Parse for --help, and os.Exit runs no defer, so the event
		// ends and the drains flush here.
		end(nil, exitCode, nil)
		os.Exit(exitCode)
	})...)
	var parsed *kong.Context
	if err == nil {
		parsed, err = parser.Parse(args)
		if err == nil {
			parsed.BindTo(ctx, (*context.Context)(nil))
			err = parsed.Run()
		}
	}
	code = codeOf(err)
	end(parsedOf(parsed, err), code, err)
	return code
}

// withExit returns the caller options with the exit function of this adapter.
func withExit(opts []kong.Option, exit func(int)) []kong.Option {
	out := append([]kong.Option{}, opts...)
	return append(out, kong.Exit(exit))
}

// panicError carries a recovered panic value and the stack of the moment it was recovered. Its
// Stack method is read by the default ErrorExtractor of core.
type panicError struct {
	value any
	stack string
}

// Error returns the panic value as a message.
func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.value) }

// Stack returns the stack of the panic.
func (e *panicError) Stack() string { return e.stack }

// parsedOf returns the parse context of one run, from the run itself or from a parse error, so
// a fault in the command line still names the command.
func parsedOf(parsed *kong.Context, err error) *kong.Context {
	if parsed != nil {
		return parsed
	}
	var parseErr *kong.ParseError
	if errors.As(err, &parseErr) {
		return parseErr.Context
	}
	return nil
}

// record writes the path, the flags, and the exit code of one run, and the level of a fault in
// the command line. A nil context records the exit code alone.
func record(ctx context.Context, parsed *kong.Context, code int) {
	if parsed != nil {
		if node := parsed.Selected(); node != nil {
			if path := node.FullPath(); path != "" {
				wlog.Set(ctx, "operation", path)
				wlog.SetGroup(ctx, "cli", "path", path)
			}
		}
		if names := flagNames(parsed); len(names) > 0 {
			wlog.SetGroup(ctx, "cli", "flags", names)
		}
	}
	wlog.SetGroup(ctx, "cli", "exit_code", code)
	if class := work.ClassOf(work.KindCommand, strconv.Itoa(code)); class == work.StatusClientError {
		wlog.SetLevel(ctx, wlog.LevelWarn)
	}
}

// isUsage reports whether one error names a fault in the command line, which kong reports as a
// parse error with the code 80.
func isUsage(err error) bool {
	var parseErr *kong.ParseError
	return errors.As(err, &parseErr)
}

// codeOf returns the process code of one error. An ExitCoder names its own code, and any other
// failure gives 1. kong gives a fault in the command line the code 80.
func codeOf(err error) int {
	if err == nil {
		return 0
	}
	var coder kong.ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	return 1
}

// flagNames returns the names of the flags that were set, never their values.
func flagNames(parsed *kong.Context) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, path := range parsed.Path {
		if path.Flag == nil || seen[path.Flag.Name] {
			continue
		}
		seen[path.Flag.Name] = true
		names = append(names, path.Flag.Name)
	}
	sort.Strings(names)
	return names
}

// flush sends the pending events of log on its own deadline, because the run context is
// often spent when the command ends.
func flush(log *wlog.Logger) {
	if log == nil {
		log = wlog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	_ = log.Flush(ctx)
}
