// This file runs the work conformance suite against the command path, and checks the exit
// code, the path, the flags, and the flush of one run.
package wlogcobra

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestCobra_C1_WorkConformance proves that the command path passes every scenario of the work
// suite.
func TestCobra_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory drives the real Execute path with one command, so a change that breaks the
// adapter fails the suite.
type workFactory struct{}

// Declare names the one kind a command run produces.
func (workFactory) Declare() workconformance.Declaration {
	return workconformance.Declaration{Kinds: []work.Kind{work.KindCommand}}
}

// Process runs one unit of work through Execute. The suite expects no panic from Process, so
// the panic of the handler, which Execute raises again, comes back as an error.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	var handlerErr error
	root := &cobra.Command{Use: "wlog", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(&cobra.Command{
		Use: "map",
		RunE: func(cmd *cobra.Command, _ []string) error {
			handlerErr = handler(cmd.Context())
			return handlerErr
		},
	})
	// The command names spell the path of the suite, so the operation matches.
	root.SetArgs([]string{"map"})
	_ = Execute(context.Background(), log, root)
	// The exit code of the run is the library's business. The suite asks for the result of
	// the handler, so the factory reports that one.
	return handlerErr
}

// TestCobra_C9_RunEErrorRecordsExitCode proves that a RunE error records exit code 1, the
// command path, and level error.
func TestCobra_C9_RunEErrorRecordsExitCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	root := newRoot()
	root.RunE = func(*cobra.Command, []string) error { return errString("boom") }

	if code := Execute(context.Background(), log, root); code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	got := lastEvent(t, rec)
	if got["operation"] != "app" || got["level"] != "error" || got["outcome"] != "error" {
		t.Errorf("operation/level/outcome = %v/%v/%v, want app/error/error", got["operation"], got["level"], got["outcome"])
	}
	if code := cliField(t, got, "exit_code"); !conformance.Equal(code, 1) {
		t.Errorf("cli.exit_code = %v, want 1", code)
	}
	if path := cliField(t, got, "path"); path != "app" {
		t.Errorf("cli.path = %v, want app", path)
	}
}

// TestCobra_C9_UnknownCommandRecordsUsageFault proves that a usage fault records exit code 2
// and level warn.
func TestCobra_C9_UnknownCommandRecordsUsageFault(t *testing.T) {
	log, rec := wlogtest.New(t)
	root := newRoot()
	root.AddCommand(&cobra.Command{Use: "sync", Run: func(*cobra.Command, []string) {}})
	root.SetArgs([]string{"nope"})

	if code := Execute(context.Background(), log, root); code != 2 {
		t.Errorf("code = %d, want 2 for a usage fault", code)
	}
	got := lastEvent(t, rec)
	if got["level"] != "warn" {
		t.Errorf("level = %v, want warn for a usage fault", got["level"])
	}
	if code := cliField(t, got, "exit_code"); !conformance.Equal(code, 2) {
		t.Errorf("cli.exit_code = %v, want 2", code)
	}
}

// TestCobra_C9_AppErrorIsNotAUsageFault proves that a RunE error whose text starts like a
// command line fault still gives exit code 1, because only the cobra and pflag texts count.
func TestCobra_C9_AppErrorIsNotAUsageFault(t *testing.T) {
	log, rec := wlogtest.New(t)
	root := newRoot()
	root.RunE = func(*cobra.Command, []string) error { return errString("requires a running database") }

	if code := Execute(context.Background(), log, root); code != 1 {
		t.Errorf("code = %d, want 1 for an app error", code)
	}
	if got := lastEvent(t, rec); got["level"] != "error" {
		t.Errorf("level = %v, want error", got["level"])
	}
}

// TestCobra_C1_SubcommandRecordsItsPath proves that a subcommand records the full path as the
// operation.
func TestCobra_SubcommandRecordsItsPath(t *testing.T) {
	log, rec := wlogtest.New(t)
	root := newRoot()
	root.AddCommand(&cobra.Command{Use: "sync", Run: func(*cobra.Command, []string) {}})
	root.SetArgs([]string{"sync"})

	if code := Execute(context.Background(), log, root); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if got := lastEvent(t, rec); got["operation"] != "app sync" {
		t.Errorf("operation = %v, want app sync", got["operation"])
	}
}

// TestCobra_C1_FlagsListTheNamesThatWereSet proves that cli.flags names the flags that were
// set, and never their values.
func TestCobra_FlagsListTheNamesThatWereSet(t *testing.T) {
	log, rec := wlogtest.New(t)
	root := newRoot()
	root.Run = func(*cobra.Command, []string) {}
	root.Flags().String("token", "", "a secret")
	root.Flags().Bool("verbose", false, "noisy")
	root.SetArgs([]string{"--token", "hunter2", "--verbose"})

	Execute(context.Background(), log, root)

	got := lastEvent(t, rec)
	flags, _ := cliField(t, got, "flags").([]any)
	if len(flags) != 2 || flags[0] != "token" || flags[1] != "verbose" {
		t.Errorf("cli.flags = %v, want token and verbose", flags)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal the event: %v", err)
	}
	if strings.Contains(string(body), "hunter2") {
		t.Errorf("the flag value reached the event: %s", body)
	}
}

// TestCobra_C1_FlushesBeforeReturn proves that the event reaches a pipeline before Execute
// returns.
func TestCobra_FlushesBeforeReturn(t *testing.T) {
	sender := &fakeSender{}
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithService("cobra-test", "0.0.1", "prod"),
		wlog.WithDrains(pipeline.Wrap(sender)),
	)
	root := newRoot()
	root.Run = func(*cobra.Command, []string) {}

	Execute(context.Background(), log, root)

	if count := sender.count(); count != 1 {
		t.Errorf("delivered %d events, want 1", count)
	}
}

// newRoot returns a quiet root command that runs.
func newRoot() *cobra.Command {
	return &cobra.Command{
		Use: "app", SilenceErrors: true, SilenceUsage: true,
		Run: func(*cobra.Command, []string) {},
	}
}

// cliField returns one field of the cli group of an event.
func cliField(t *testing.T, got map[string]any, key string) any {
	t.Helper()
	cli, _ := got["cli"].(map[string]any)
	if cli == nil {
		t.Fatalf("the cli group is missing from %v", got)
	}
	return cli[key]
}

// lastEvent returns the only recorded event, and stops the test when the run recorded none.
func lastEvent(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	return got
}

// errString is the plain error a test returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
