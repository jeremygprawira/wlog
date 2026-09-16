// Command verifyplan runs the Verify command of every task in tasks/plan.md.
//
// A plan is a promise, and the promise of a task is its Verify line. The command
// reads each line, runs the command in it at the repository root, and fails
// when the command fails or when its -run pattern matches no test. The second
// check catches the plan's most common rot: a task whose tests were renamed, so
// the command still exits 0 while it proves nothing.
//
// A Verify line that holds no command, such as one that describes a push, is
// skipped. The -only flag limits the run to the tasks whose id holds a string.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// planPath is the plan that the command reads.
const planPath = "tasks/plan.md"

// verifyLine matches a Verify line and captures the command in backticks.
var verifyLine = regexp.MustCompile(`\*\*Verify:\*\*\s*(.*)$`)

// backticked matches the first backticked span of a line.
var backticked = regexp.MustCompile("`([^`]+)`")

// taskHeading matches a task heading and captures its id.
var taskHeading = regexp.MustCompile(`^#{3,4}\s+([0-9A-Za-z-]+)`)

// main finds the workspace root, then checks the Verify commands.
func main() {
	only := flag.String("only", "", "run only the tasks whose id holds this string")
	plan := flag.String("plan", planPath, "the plan to read")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if err := run(root, *plan, *only, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "verifyplan:", err)
	os.Exit(1)
}

// task is one Verify line, with the id of the task it belongs to.
type task struct {
	id      string
	line    int
	command string
}

// run checks every task and prints one line per problem.
//
// It returns an error when it finds at least one problem, so a broken plan fails
// CI. only limits the run to the tasks whose id holds that string.
func run(root, plan, only string, out io.Writer) error {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	path := plan
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, plan)
	}
	tasks, err := parse(path, only)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return fmt.Errorf("%s holds no Verify command", plan)
	}

	var problems []string
	for _, t := range tasks {
		text, err := verify(root, t.command)
		if err == nil {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s:%d: VP1: %v: %s\n  %s\n%s",
			plan, t.line, err, t.id, t.command, indent(text)))
	}
	sort.Strings(problems)
	for _, p := range problems {
		if _, err := fmt.Fprintln(out, p); err != nil {
			return err
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d Verify command(s) fail", len(problems))
	}
	return nil
}

// parse returns the tasks of a plan whose Verify line holds a command.
//
// only, when it is not empty, keeps the tasks whose id holds that string. A
// Verify line that holds no command, such as one that describes a push, is
// skipped, because this command cannot run it.
func parse(path, only string) ([]task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tasks []task
	id := ""
	for i, line := range strings.Split(string(data), "\n") {
		if m := taskHeading.FindStringSubmatch(line); m != nil {
			id = m[1]
			continue
		}
		m := verifyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		command := backticked.FindStringSubmatch(m[1])
		if command == nil || !runnable(command[1]) {
			continue
		}
		if only != "" && !strings.Contains(id, only) {
			continue
		}
		tasks = append(tasks, task{id: id, line: i + 1, command: command[1]})
	}
	return tasks, nil
}

// runnable reports whether a backticked span is a whole command rather than a
// fragment of prose. A plan often names a command in the middle of an English
// sentence, and this command cannot run a fragment.
func runnable(command string) bool {
	for _, word := range []string{"go test", "go run", "go vet", "make "} {
		if strings.Contains(command, word) {
			return true
		}
	}
	return strings.HasPrefix(command, "cd ")
}

// verify runs one command at the repository root and fails when it fails or when
// its -run pattern matches no test.
func verify(root, command string) ([]byte, error) {
	command = addVerbose(command)
	cmd := exec.CommandContext(context.Background(), "bash", "-c", command)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("command failed: %w", err)
	}
	if strings.Contains(string(out), "no tests to run") {
		return out, fmt.Errorf("the -run pattern matches no test")
	}
	return out, nil
}

// addVerbose asks the go command for the verbose form, so a test that never ran
// shows up in the output.
func addVerbose(command string) string {
	if signal := "go test "; strings.Contains(command, signal) {
		return strings.Replace(command, signal, signal+"-v ", 1)
	}
	return command
}

// indent puts four spaces before every line of a command output, so a report
// line stays readable.
func indent(text []byte) string {
	lines := strings.Split(strings.TrimRight(string(text), "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}
