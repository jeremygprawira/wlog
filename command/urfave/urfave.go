// This file holds the command runner, the exit code rule, and the mapping from one run onto
// one unit of work.
package wlogurfave

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// flushTimeout is the budget of the flush that runs after every command.
const flushTimeout = 2 * time.Second

// Run runs cmd with args in the os.Args form, gives the run one command event, flushes the drains, and returns the
// code for os.Exit. urfave exits inside Run for an exit-coder error, so Run sets the root
// ExitErrHandler to record and flush first, and then calls cli.HandleExitCoder. A handler the
// caller already set runs instead, which lets a test keep the process. A nil Logger means
// wlog.Default.
func Run(ctx context.Context, log *wlog.Logger, cmd *cli.Command, args []string) int {
	code := 0
	// The leaf is resolved before the run, because the exit handler of urfave receives the
	// root command, and the unit names the operation before any child starts.
	leaf := leafOf(cmd, args)
	ctx, handle := work.Start(ctx, log, work.Unit{Kind: work.KindCommand, Operation: leaf.FullName()})
	ended := false
	end := func(cmd *cli.Command, err error) {
		if ended {
			return
		}
		ended = true
		record(ctx, cmd, code)
		handle.End(err)
		flush(log)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			code = 1
			wlog.Error(ctx, &panicError{value: recovered, stack: string(debug.Stack())})
			end(leaf, nil)
			panic(recovered)
		}
	}()

	root := cmd.Root()
	previous := root.ExitErrHandler
	root.ExitErrHandler = func(handlerCtx context.Context, handlerCmd *cli.Command, err error) {
		code = exitCode(err)
		end(leaf, err)
		if previous != nil {
			previous(handlerCtx, handlerCmd, err)
			return
		}
		cli.HandleExitCoder(err)
	}

	err := cmd.Run(ctx, args)
	if err != nil {
		code = exitCode(err)
	}
	end(leaf, err)
	return code
}

// leafOf returns the command that args select, walking the tree by name. The first argument is
// the program name, and an argument that names no child ends the walk, because it is a value.
func leafOf(cmd *cli.Command, args []string) *cli.Command {
	leaf := cmd
	if len(args) == 0 {
		return leaf
	}
	skipValue := false
	for _, arg := range args[1:] {
		if skipValue {
			skipValue = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && takesValue(leaf, arg) {
				skipValue = true
			}
			continue
		}
		next := leaf.Command(arg)
		if next == nil {
			break
		}
		leaf = next
	}
	return leaf
}

// takesValue reports whether one flag argument expects a value, which decides whether the
// token after it is a value or a command.
func takesValue(cmd *cli.Command, arg string) bool {
	name := strings.TrimLeft(arg, "-")
	for _, node := range cmd.Lineage() {
		for _, flag := range node.Flags {
			for _, candidate := range flag.Names() {
				if candidate != name {
					continue
				}
				if withValue, ok := flag.(interface{ TakesValue() bool }); ok {
					return withValue.TakesValue()
				}
				return false
			}
		}
	}
	return false
}

// process runs one unit of work through the event path of this adapter, with a recovered panic
// as an error, so a test continues after the panic scenario.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// record writes the path, the flags, and the exit code of one run, and the level of a usage
// fault.
func record(ctx context.Context, cmd *cli.Command, code int) {
	if cmd != nil {
		if path := cmd.FullName(); path != "" {
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

// exitCode returns the process code of one error. An ExitCoder names its code, a MultiError
// takes the code of its last ExitCoder, a usage fault gives 2, and any other failure gives 1.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var multi cli.MultiError
	if errors.As(err, &multi) {
		code := 1
		for _, one := range multi.Errors() {
			var coder cli.ExitCoder
			if errors.As(one, &coder) {
				code = coder.ExitCode()
			}
		}
		return code
	}
	var coder cli.ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	if usageFault(err) {
		return 2
	}
	return 1
}

// usagePrefixes holds the message prefixes that urfave writes for a fault in the command line.
var usagePrefixes = []string{
	"flag provided but not defined", "flag needs an argument", "invalid value",
	"Required flag", "Required flags", "unexpected argument",
}

// usageFault reports whether one error names a fault in the command line. urfave names no
// usage error type, so the message prefixes of its own faults decide.
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
func flagNames(cmd *cli.Command) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, node := range cmd.Lineage() {
		for _, flag := range node.Flags {
			if !flag.IsSet() {
				continue
			}
			all := flag.Names()
			if len(all) == 0 || seen[all[0]] {
				continue
			}
			seen[all[0]] = true
			names = append(names, all[0])
		}
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
