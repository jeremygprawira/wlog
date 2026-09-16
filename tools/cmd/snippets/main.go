// Command snippets compiles every fenced Go block in the documentation, and runs
// the ones that ask to run.
//
// A document that shows code makes a promise, and a reader who copies the code
// must get a working program. The command extracts every fenced block whose info
// string starts with "go", writes each one into its own file of a scratch
// module, and builds it. A block whose info string holds "run" also runs, and
// its output must equal the next fenced text block, which is how a document
// shows a sample of its own output.
//
// The scratch module requires every module of the workspace, with a replace to
// the local directory, so a snippet that imports echo, gin, or wlog resolves
// without a network call.
//
// A file listed in tools/snippets-known-bad.txt is skipped. That list holds the
// blocks that do not compile today, and the documentation tasks remove the lines
// as they rewrite the files.
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

// knownBadPath is the list of files whose blocks the command skips.
const knownBadPath = "tools/snippets-known-bad.txt"

// block is one fenced code block.
type block struct {
	file string // path relative to the root, with a leading "./"
	line int    // the line of the first line of code
	info string // the info string of the fence, such as "go run"
	code string
	text string // the next fenced text block, for a block that runs
}

// fence matches one fenced block and captures its info string and body.
var fence = regexp.MustCompile("(?ms)^```([^\n]*)\n(.*?)^```[ \t]*$")

// main finds the workspace root and exits 1 when any block fails.
func main() {
	only := flag.String("only", "", "check only the files whose path holds this string")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if err := run(root, *only, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "snippets:", err)
	os.Exit(1)
}

// run checks every Go block and prints one line per problem.
func run(root, only string, out io.Writer) error {
	skip, err := readList(filepath.Join(root, knownBadPath))
	if err != nil {
		return err
	}
	blocks, err := collect(root)
	if err != nil {
		return err
	}

	scratch, err := newScratch(root)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch.dir) }()

	var problems []string
	checked := 0
	for i, b := range blocks {
		if skip[b.file] || (only != "" && !strings.Contains(b.file, only)) {
			continue
		}
		checked++
		problem := checkBlock(scratch, i, b)
		if problem != "" {
			problems = append(problems, problem)
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		if _, err := fmt.Fprintln(out, p); err != nil {
			return err
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d snippet(s) fail", len(problems))
	}
	if checked == 0 {
		return fmt.Errorf("no Go block found")
	}
	return nil
}

// checkBlock builds one block, and runs it when its info string asks for that.
// It returns an empty string when the block is good.
func checkBlock(scratch *scratchModule, index int, b block) string {
	dir := filepath.Join(scratch.dir, fmt.Sprintf("snippet%03d", index))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Sprintf("%s:%d: SNIP0: %v", b.file, b.line, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.code), 0o600); err != nil {
		return fmt.Sprintf("%s:%d: SNIP0: %v", b.file, b.line, err)
	}

	verb := "build"
	args := []string{"build", "-o", os.DevNull, "./" + filepath.Base(dir)}
	if runs(b) {
		verb = "run"
		args = []string{"run", "./" + filepath.Base(dir)}
	}
	text, err := scratch.goCommand(args...)
	if err != nil {
		return fmt.Sprintf("%s:%d: SNIP1: the block does not %s\n%s", b.file, b.line, verb, indent(text))
	}
	if !runs(b) {
		return ""
	}
	if b.text == "" {
		return fmt.Sprintf("%s:%d: SNIP3: the run block has no text block after it", b.file, b.line)
	}
	if got, want := strings.TrimSpace(string(text)), strings.TrimSpace(b.text); got != want {
		return fmt.Sprintf("%s:%d: SNIP2: the output differs\n  got:\n%s\n  want:\n%s",
			b.file, b.line, indent([]byte(got)), indent([]byte(want)))
	}
	return ""
}

// runs reports whether a block asks to run.
func runs(b block) bool {
	for _, field := range strings.Fields(b.info) {
		if field == "run" {
			return true
		}
	}
	return false
}

// collect returns every Go block of the documentation.
//
// It reads the markdown files of the repository and skips the working notes of
// the maintainer, the test fixtures, and the tools module.
func collect(root string) ([]block, error) {
	var blocks []block
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "vendor" || name == "testdata" || name == "tasks" || name == "tools" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		found, err := blocksOf(root, path)
		if err != nil {
			return err
		}
		blocks = append(blocks, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A stable order keeps the file names of the scratch module stable too.
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].file != blocks[j].file {
			return blocks[i].file < blocks[j].file
		}
		return blocks[i].line < blocks[j].line
	})
	return blocks, nil
}

// blocksOf returns the Go blocks of one markdown file. A text block that follows
// a block that runs becomes its expected output.
func blocksOf(root, path string) ([]block, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body := string(data)
	matches := fence.FindAllStringSubmatchIndex(body, -1)

	var out []block
	for i, m := range matches {
		info := body[m[2]:m[3]]
		code := body[m[4]:m[5]]
		if !strings.HasPrefix(strings.TrimSpace(info), "go") {
			continue
		}
		b := block{
			file: rel(root, path),
			line: strings.Count(body[:m[0]], "\n") + 1,
			info: strings.TrimSpace(info),
			code: code,
		}
		// The text block directly after a block that runs holds its output.
		if i+1 < len(matches) && strings.TrimSpace(body[matches[i+1][2]:matches[i+1][3]]) == "text" {
			b.text = body[matches[i+1][4]:matches[i+1][5]]
		}
		out = append(out, b)
	}
	return out, nil
}

// readList reads a list of paths, one per line. A missing list is not an error,
// because the lists shrink to nothing as the documentation tasks land.
func readList(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line != "" {
			skip[line] = true
		}
	}
	return skip, nil
}

// scratchModule is a throwaway module whose build list holds every module of the
// workspace through a replace to the local directory.
type scratchModule struct {
	dir string
}

// newScratch writes the scratch module and returns it.
//
// A root that holds a go.work file contributes every module of the workspace
// through a replace, so a snippet that imports wlog, echo, or gin resolves. A
// fixture directory holds no workspace, and its snippets then stand alone.
func newScratch(root string) (*scratchModule, error) {
	var mods []workspace.Module
	if _, err := os.Stat(filepath.Join(root, "go.work")); err == nil {
		var err error
		if mods, err = workspace.Modules(root); err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp("", "wlog-snippets-")
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString("module snippet\n\ngo 1.21\n\nrequire (\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "\t%s v0.0.0\n", m.Path)
	}
	b.WriteString(")\n\nreplace (\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "\t%s => %s\n", m.Path, filepath.Join(root, filepath.FromSlash(m.Dir)))
	}
	b.WriteString(")\n")

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(b.String()), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &scratchModule{dir: dir}, nil
}

// goCommand runs one go command in the scratch module.
//
// The module has no go.sum, so the command runs with -mod=mod and fills the
// build list from the module cache of the repository. A build writes to the null
// device, because the snippet directory already holds the name of the package.
func (s *scratchModule) goCommand(args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "go", args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	return cmd.CombinedOutput()
}

// indent puts four spaces before every line of a command output.
func indent(text []byte) string {
	lines := strings.Split(strings.TrimRight(string(text), "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

// rel returns a path relative to the root, with a leading "./".
func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return "./" + filepath.ToSlash(r)
}
