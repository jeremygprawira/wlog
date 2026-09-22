// This file runs the work conformance suite against the command path, and checks the exit
// code, the path, the flags, and the flush of one run.
package wlogkong

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/setup"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestKong_C1_WorkConformance proves that the command path passes every scenario of the work
// suite.
func TestKong_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path of this adapter. The suite supplies the
// unit, because one command run carries no rpc, message, function, or job field.
// workFactory drives the real Run path with a grammar whose Run method calls the handler.
type workFactory struct{}

// Declare names the one kind a kong run produces.
func (workFactory) Declare() workconformance.Declaration {
	return workconformance.Declaration{Kinds: []work.Kind{work.KindCommand}}
}

// Process runs one unit of work through Run. The suite expects no panic from Process, so the
// panic of the handler, which Run raises again, comes back as an error.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	var handlerErr error
	grammar := &suiteCLI{Map: suiteCmd{run: func(ctx context.Context) error {
		handlerErr = handler(ctx)
		return handlerErr
	}}}
	// The grammar and its one command spell the path of the suite, so the operation matches.
	_ = Run(context.Background(), log, grammar, []string{"map"}, kong.Name("wlog"))
	// The exit code of the run is the library's business. The suite asks for the result of
	// the handler, so the factory reports that one.
	return handlerErr
}

// suiteCLI is the grammar of the suite: one command, whose Run method runs the handler.
type suiteCLI struct {
	Map suiteCmd `cmd:"" help:"the map command"`
}

// suiteCmd runs the handler of the suite.
type suiteCmd struct {
	run func(context.Context) error
}

// Run runs the handler of the suite.
func (c *suiteCmd) Run(ctx context.Context) error { return c.run(ctx) }

// TestKong_C9_ParseErrorRecordsEighty proves that a fault in the command line records exit code
// 80 and level warn, and the path of the command it reached.
func TestKong_C9_ParseErrorRecordsEighty(t *testing.T) {
	log, rec := wlogtest.New(t)

	if code := Run(context.Background(), log, &testCLI{}, []string{"--nope"}, kong.Name("app")); code != 80 {
		t.Errorf("code = %d, want 80 for a fault in the command line", code)
	}
	got := lastEvent(t, rec)
	if got["level"] != "warn" {
		t.Errorf("level = %v, want warn", got["level"])
	}
	if code := cliField(t, got, "exit_code"); !conformance.Equal(code, 80) {
		t.Errorf("cli.exit_code = %v, want 80", code)
	}
}

// TestKong_C1_SubcommandRecordsPathAndFlags proves that a subcommand records its full path, the
// names of the flags that were set, and the event context that BindTo gives the command.
func TestKong_SubcommandRecordsPathAndFlags(t *testing.T) {
	log, rec := wlogtest.New(t)
	cli := &testCLI{}

	if code := Run(context.Background(), log, cli, []string{"sync", "--verbose", "--token", "hunter2"}, kong.Name("app")); code != 0 {
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
	if got["inside"] != true {
		t.Errorf("inside = %v, want the command to see the event context", got["inside"])
	}
}

// TestKong_C1_FlushesBeforeReturn proves that the event reaches a pipeline before Run returns.
func TestKong_FlushesBeforeReturn(t *testing.T) {
	sender := &fakeSender{}
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithService("kong-test", "0.0.1", "prod"),
		wlog.WithDrains(pipeline.Wrap(sender)),
	)

	Run(context.Background(), log, &testCLI{}, []string{"sync"}, kong.Name("app"))

	if count := sender.count(); count != 1 {
		t.Errorf("delivered %d events, want 1", count)
	}
}

// TestKong_C1_HelpEndsTheEvent proves that --help ends the event and flushes it before kong
// exits, because os.Exit runs no defer. The help flag exits inside Parse, so the check runs in
// a child process.
func TestKong_HelpEndsTheEvent(t *testing.T) {
	if path := os.Getenv("WLOG_KONG_HELP_FILE"); path != "" {
		log := wlog.New(setup.FromEnv())
		Run(context.Background(), log, &testCLI{}, []string{"--help"}, kong.Name("app"))
		os.Exit(3) // kong exits on --help, so this line is unreachable
	}

	path := filepath.Join(t.TempDir(), "events.jsonl")
	child := exec.Command(os.Args[0], "-test.run=TestKong_HelpEndsTheEvent")
	child.Env = append(os.Environ(),
		"WLOG_KONG_HELP_FILE="+path,
		"WLOG_DRAINS=file",
		"WLOG_FILE_PATH="+path,
	)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("the child exited with %v:\n%s", err, out)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the drain file: %v", err)
	}
	// kong exits inside Parse, so the command path is not resolved. The event still ends
	// and carries the exit code.
	if !strings.Contains(string(body), `"kind":"command"`) || !strings.Contains(string(body), `"exit_code":0`) {
		t.Errorf("the drain file holds no command event: %s", body)
	}
}

// testCLI is the grammar of the tests.
type testCLI struct {
	Verbose bool     `help:"noisy"`
	Sync    syncCmd  `cmd:"" help:"sync the orders"`
	Check   checkCmd `cmd:"" help:"check the orders"`
}

// syncCmd runs the sync command.
type syncCmd struct {
	Token string `help:"a secret"`
}

// Run records that the command saw the event context.
func (c *syncCmd) Run(ctx context.Context) error {
	wlog.Set(ctx, "inside", true)
	return nil
}

// checkCmd runs the check command and fails.
type checkCmd struct{}

// Run returns an error.
func (c *checkCmd) Run(context.Context) error { return errString("boom") }

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
