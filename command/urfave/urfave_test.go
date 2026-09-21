// This file runs the work conformance suite against the command path, and checks the exit
// code, the path, the flags, and the flush of one run.
package wlogurfave

import (
	"context"
	"io"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestUrfave_C1_WorkConformance proves that the command path passes every scenario of the work
// suite.
func TestUrfave_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path of this adapter. The suite supplies the
// unit, because one command run carries no rpc, message, function, or job field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestUrfave_C9_ExitCoderRecordsTheCode proves that an exit-coder error records its own code,
// the path, and level error, and the process exits with that code.
func TestUrfave_C9_ExitCoderRecordsTheCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	cmd := newCommand()
	cmd.Action = func(context.Context, *cli.Command) error { return cli.Exit("boom", 3) }
	exited := 0
	quietExit(t, &exited)

	if code := Run(context.Background(), log, cmd, []string{"app"}); code != 3 {
		t.Errorf("code = %d, want 3", code)
	}
	if exited != 3 {
		t.Errorf("the process exited with %d, want 3", exited)
	}
	got := lastEvent(t, rec)
	if got["operation"] != "app" || got["level"] != "error" {
		t.Errorf("operation/level = %v/%v, want app/error", got["operation"], got["level"])
	}
	if code := cliField(t, got, "exit_code"); !conformance.Equal(code, 3) {
		t.Errorf("cli.exit_code = %v, want 3", code)
	}
}

// TestUrfave_C9_UsageFaultRecordsTwo proves that a fault in the command line records exit code
// 2 and level warn.
func TestUrfave_C9_UsageFaultRecordsTwo(t *testing.T) {
	log, rec := wlogtest.New(t)
	cmd := newCommand()

	if code := Run(context.Background(), log, cmd, []string{"app", "--nope"}); code != 2 {
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

// TestUrfave_C1_SubcommandRecordsItsPathAndFlags proves that a subcommand records its full
// path, and the names of the flags that were set.
func TestUrfave_C1_SubcommandRecordsItsPathAndFlags(t *testing.T) {
	log, rec := wlogtest.New(t)
	cmd := newCommand()
	cmd.Commands = []*cli.Command{{
		Name: "sync",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "verbose"},
			&cli.StringFlag{Name: "token"},
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}}

	if code := Run(context.Background(), log, cmd, []string{"app", "sync", "--verbose", "--token", "hunter2"}); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	got := lastEvent(t, rec)
	if got["operation"] != "app sync" {
		t.Errorf("operation = %v, want app sync", got["operation"])
	}
	flags, _ := cliField(t, got, "flags").([]any)
	if len(flags) != 2 || flags[0] != "token" || flags[1] != "verbose" {
		t.Errorf("cli.flags = %v, want token and verbose", flags)
	}
}

// TestUrfave_C1_FlushesBeforeReturn proves that the event reaches a pipeline before Run
// returns.
func TestUrfave_C1_FlushesBeforeReturn(t *testing.T) {
	sender := &fakeSender{}
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithService("urfave-test", "0.0.1", "prod"),
		wlog.WithDrains(pipeline.Wrap(sender)),
	)
	cmd := newCommand()

	Run(context.Background(), log, cmd, []string{"app"})

	if count := sender.count(); count != 1 {
		t.Errorf("delivered %d events, want 1", count)
	}
}

// TestUrfave_C1_PanicRecordsStackAndPanics proves that a panicking command records one error
// event with a stack, and the panic continues.
func TestUrfave_C1_PanicRecordsStackAndPanics(t *testing.T) {
	log, rec := wlogtest.New(t)
	cmd := newCommand()
	cmd.Action = func(context.Context, *cli.Command) error { panic("boom") }

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the wrapper did not panic again")
			}
		}()
		Run(context.Background(), log, cmd, []string{"app"})
	}()

	info, _ := lastEvent(t, rec)["error"].(map[string]any)
	if info == nil || info["stack"] == nil {
		t.Errorf("error = %v, want the recovered stack", info)
	}
}

// newCommand returns a quiet root command that runs.
func newCommand() *cli.Command {
	return &cli.Command{
		Name:      "app",
		ErrWriter: io.Discard,
		Action:    func(context.Context, *cli.Command) error { return nil },
	}
}

// quietExit replaces the process exit of urfave for one test, so the test keeps running.
func quietExit(t *testing.T, code *int) {
	t.Helper()
	previousExit, previousWriter := cli.OsExiter, cli.ErrWriter
	cli.OsExiter = func(c int) { *code = c }
	cli.ErrWriter = io.Discard
	t.Cleanup(func() {
		cli.OsExiter = previousExit
		cli.ErrWriter = previousWriter
	})
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
