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
	"strconv"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// planPath is the plan that the command reads.
const planPath = "tasks/plan.md"

// verifyLine matches a Verify line and captures the command in backticks.
var verifyLine = regexp.MustCompile(`\*\*Verify:\*\*\s*(.*)$`)

// backticked matches the first backticked span of a line.
var backticked = regexp.MustCompile("`([^`]+)`")

// taskHeading matches a task heading and captures its id. Every task id holds a digit, so
// a heading such as "Track A" is a section and not a task.
var taskHeading = regexp.MustCompile(`^#{3,4}\s+([0-9A-Za-z-]*[0-9][0-9A-Za-z-]*)`)

// auditID matches an audit id, such as PAR-33 or SPEC-G2, wherever it appears. A plan task
// carries a prefix, as in 10-AUD-2, and the run of letters and digits before the dash keeps
// that form out.
var auditID = regexp.MustCompile(`(?:^|[^0-9A-Za-z-])([A-Z]{2,6}-[A-Z]?[0-9]+)\b`)

// tableRow matches the first cell of a markdown table row.
var tableRow = regexp.MustCompile(`^\|\s*([A-Z]{2,6}-[A-Z]?[0-9]+)\s*\|`)

// idRange matches a range of ids, as in "REL-1 to REL-8", which the changelog uses so a
// long list of closed ids stays readable.
var idRange = regexp.MustCompile(`\b([A-Z]{2,6}-[A-Z]?)([0-9]+) to ([A-Z]{2,6}-[A-Z]?)([0-9]+)\b`)

// changelogPath is the file that records the decision of an audit id that no plan task
// closes.
const changelogPath = "CHANGELOG.md"

// main finds the workspace root, then checks the Verify commands.
func main() {
	only := flag.String("only", "", "run only the tasks whose id holds this string")
	plan := flag.String("plan", planPath, "the plan to read")
	audit := flag.String("audit", "", "map every id of an audit page to the plan task that closes it")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if *audit != "" {
		if err := auditRun(root, *audit, *plan, os.Stdout); err != nil {
			fail(err)
		}
		return
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
	// A "go test ./..." Verify command prints "no tests to run" for every sibling
	// package the -run pattern misses, which is normal, not a failure. Only "=== RUN"
	// says a test in some package actually matched and ran, from the -v flag
	// addVerbose adds. A command with no "go test" in it, such as "go run" or
	// "make", never prints that marker either way, so it is judged on its exit
	// code alone.
	if isGoTest(command) && !strings.Contains(string(out), "=== RUN") {
		return out, fmt.Errorf("the -run pattern matches no test")
	}
	return out, nil
}

// isGoTest reports whether command is a go test invocation, the only kind addVerbose
// makes print "=== RUN" for a matched test.
func isGoTest(command string) bool {
	return strings.Contains(command, "go test ")
}

// addVerbose asks the go command for the verbose form, so a test that never ran
// shows up in the output. Every go test of a compound command gets the flag,
// because the test that runs may not be the first one: a command that checks a
// package with no tests and then runs the pattern would otherwise look empty.
func addVerbose(command string) string {
	return strings.ReplaceAll(command, "go test ", "go test -v ")
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

// auditRun maps every id of an audit page to the plan task that closes it, and it runs the
// Verify command of each closing task.
//
// An id that no task names is a gap: the audit found the problem and nothing closes it. A
// closing task without a Verify command, or one whose command fails, leaves the id without
// a passing test. A task whose own command is this one is skipped, because the audit page
// closes the task that checks the audit.
func auditRun(root, audit, plan string, out io.Writer) error {
	ids, err := auditIDs(join(root, audit))
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("%s holds no audit id", audit)
	}
	named, err := planNames(join(root, plan))
	if err != nil {
		return err
	}
	recorded, err := recordedIDs(join(root, changelogPath))
	if err != nil {
		return err
	}
	tasks, err := parse(join(root, plan), "")
	if err != nil {
		return err
	}
	commands := map[string]string{}
	for _, t := range tasks {
		commands[t.id] = t.command
	}
	// The task that closes an id is the first task that names it and holds a Verify
	// command. A task that names the id without a command cannot map it to a test.
	closing := map[string]string{}
	for id, owners := range named {
		for _, owner := range owners {
			if _, ok := commands[owner]; ok {
				closing[id] = owner
				break
			}
		}
		if closing[id] == "" && len(owners) > 0 {
			closing[id] = owners[0]
		}
	}

	var problems []string
	checked := map[string]bool{}
	bad := map[string]bool{}
	skipped := 0
	recordedCount := 0
	for _, id := range ids {
		task := closing[id]
		if task == "" {
			// A decision that the changelog records closes an id that no task closes,
			// which is the case for a spec question the plan answers by a rule.
			if recorded[id] {
				recordedCount++
				continue
			}
			problems = append(problems, fmt.Sprintf("%s: AUD1: no plan task and no changelog decision names %s", audit, id))
			continue
		}
		command, ok := commands[task]
		if !ok {
			if recorded[id] {
				recordedCount++
				continue
			}
			problems = append(problems, fmt.Sprintf("%s: AUD2: the task %s closes %s and holds no Verify command", audit, task, id))
			continue
		}
		if strings.Contains(command, "verifyplan") {
			skipped++
			continue
		}
		if !checked[task] {
			// The first id of a task carries the command and its output, so a failure is
			// read once. Every later id of the same task carries one short line.
			checked[task] = true
			if text, err := verify(root, command); err != nil {
				bad[task] = true
				problems = append(problems, fmt.Sprintf("%s: AUD3: the task %s closes %s and its Verify command fails: %v\n  %s\n%s",
					audit, task, id, err, command, indent(text)))
			}
			continue
		}
		if bad[task] {
			problems = append(problems, fmt.Sprintf("%s: AUD3: the task %s closes %s and its Verify command fails", audit, task, id))
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		if _, err := fmt.Fprintln(out, p); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "%s: %d id(s), %d task(s) checked, %d id(s) by a recorded decision, %d task(s) skipped\n",
		audit, len(ids), len(checked), recordedCount, skipped); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d audit id(s) hold a problem", len(problems))
	}
	return nil
}

// auditIDs returns every id that an audit page defines, sorted and without repeats.
//
// The page defines an id in a heading, as in "#### AUD-1", or in the first cell of a table
// row, as in "| PAR-1 |". An id that the page only mentions in prose belongs to another
// page and is not an id of this audit.
func auditIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if m := taskHeading.FindStringSubmatch(line); m != nil {
			add(m[1])
			continue
		}
		if m := tableRow.FindStringSubmatch(line); m != nil {
			add(m[1])
		}
	}
	sort.Strings(out)
	return out, nil
}

// planNames returns the tasks that name each audit id, in the order the plan gives them.
func planNames(path string) (map[string][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	named := map[string][]string{}
	id := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := taskHeading.FindStringSubmatch(line); m != nil {
			id = m[1]
			continue
		}
		if id == "" {
			continue
		}
		for _, m := range auditID.FindAllStringSubmatch(line, -1) {
			owners := named[m[1]]
			if len(owners) > 0 && owners[len(owners)-1] == id {
				continue
			}
			named[m[1]] = append(owners, id)
		}
	}
	return named, nil
}

// join returns an absolute path for a path that is relative to the root.
func join(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

// recordedIDs returns every audit id that the changelog names, with a range such as
// "REL-1 to REL-8" expanded.
func recordedIDs(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	text := string(data)
	out := map[string]bool{}
	for _, m := range idRange.FindAllStringSubmatch(text, -1) {
		// The two ids of a range share a prefix, as in "REL-1 to REL-8".
		if m[1] != m[3] {
			continue
		}
		first, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		last, err := strconv.Atoi(m[4])
		if err != nil || last < first || last-first > 200 {
			continue
		}
		for n := first; n <= last; n++ {
			out[fmt.Sprintf("%s%d", m[1], n)] = true
		}
	}
	for _, m := range auditID.FindAllStringSubmatch(text, -1) {
		out[m[1]] = true
	}
	return out, nil
}
