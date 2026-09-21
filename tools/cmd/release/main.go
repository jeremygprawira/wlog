// Command release prepares a release, and applies it after the maintainer
// confirms the tag list.
//
// A release moves a repository of modules, not a single module, so the order
// matters: the root module is tagged first, and a module waits until every
// module it requires has a tag. The command prints that order, the require lines
// each module must change, and the result of apidiff against the previous tag of
// each module.
//
// The command starts in dry-run mode, which changes nothing. A real run needs
// -dry-run=false, and it still asks the maintainer to type the tag list before
// it touches the repository. The command never pushes, and it never tags before
// the typed list matches.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// versionFile holds the version of the last release.
const versionFile = "tools/version.txt"

// apidiff is the tool that compares the exported API of two versions.
const apidiff = "golang.org/x/exp/cmd/apidiff"

// main prints the plan, and applies it when the maintainer confirms.
func main() {
	version := flag.String("version", "", "the version to release, such as v0.5.0")
	dryRun := flag.Bool("dry-run", true, "print the plan and change nothing")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	if err := run(root, *version, *dryRun, os.Stdin, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "release:", err)
	os.Exit(1)
}

// say writes one report line and ignores a broken writer, because a release plan
// is read on a terminal and never piped into a decision.
func say(out io.Writer, line string) {
	_, _ = fmt.Fprintln(out, line)
}

// step is one module of the release plan.
type step struct {
	module      workspace.Module
	lastTag     string   // the previous git tag, empty for a new module
	lastVersion string   // the previous version on its own, as the module proxy writes it
	changes     []string // the require lines to rewrite
}

// run prints the plan for a version and, without dry-run, applies it.
//
// It reads and writes nothing until the maintainer types the tag list, so a
// mistake in an argument never leaves a half-finished release behind.
func run(root, version string, dryRun bool, in io.Reader, out io.Writer) error {
	if version == "" {
		return fmt.Errorf("give -version, such as v0.5.0")
	}
	old, err := readVersion(filepath.Join(root, versionFile))
	if err != nil {
		return err
	}
	steps, err := plan(root, old, version)
	if err != nil {
		return err
	}

	say(out, fmt.Sprintf("release %s (from %s)\n", version, old))
	say(out, "\ntags, in dependency order:")
	for _, s := range steps {
		say(out, fmt.Sprintf("  %s", tag(s.module, version)))
	}
	if !dryRun {
		say(out, "\nrequire updates:")
		any := false
		for _, s := range steps {
			for _, change := range s.changes {
				any = true
				say(out, fmt.Sprintf("  %s: %s", s.module.Dir, change))
			}
		}
		if !any {
			say(out, "  none")
		}
	}
	if dryRun {
		say(out, "\nAPI against the previous tag:")
		for _, s := range steps {
			report, err := apiDiff(root, s)
			if err != nil {
				say(out, fmt.Sprintf("  %s: %v", s.module.Dir, err))
				if reason := oneLine(report); reason != "" {
					say(out, "    "+reason)
				}
				continue
			}
			say(out, fmt.Sprintf("  %s: %s", s.module.Dir, oneLine(report)))
		}
		say(out, "\nthe dry run changed nothing. A real run needs -dry-run=false.")
		return nil
	}

	expected := make([]string, 0, len(steps))
	for _, s := range steps {
		expected = append(expected, tag(s.module, version))
	}
	say(out, fmt.Sprintf("\ntype these %d tags, one per line, to apply the release:", len(expected)))
	typed, err := readLines(in, len(expected))
	if err != nil {
		return err
	}
	if len(typed) != len(expected) {
		return fmt.Errorf("the typed list differs from the plan: it holds %d tags, want %d", len(typed), len(expected))
	}
	for i, want := range expected {
		if typed[i] != want {
			return fmt.Errorf("the typed list differs from the plan at line %d: %q, want %q", i+1, typed[i], want)
		}
	}
	return apply(root, version, steps, out)
}

// plan returns the release steps: the root module first, then every other module
// after the modules it requires.
//
// The order is a topological sort of the require graph, with the root module
// first, because a user imports the root module and reads its tag. A cycle would
// stop the release, and go.work never holds one.
func plan(root, old, version string) ([]step, error) {
	mods, err := workspace.Modules(root)
	if err != nil {
		return nil, err
	}
	order, err := tagOrder(mods)
	if err != nil {
		return nil, err
	}

	// A sibling is a module of this workspace, so the update never touches a
	// library that lives somewhere else.
	siblings := map[string]bool{}
	for _, m := range mods {
		siblings[m.Path] = true
	}

	steps := make([]step, 0, len(order))
	for _, m := range order {
		// A sub-module carries its directory in its git tag, and the proxy
		// knows it by the plain version, so the step holds both.
		s := step{module: m, lastTag: lastTag(root, m, old), lastVersion: oldVersion(old)}
		for _, req := range m.Requires {
			if !siblings[req] {
				continue
			}
			if reqVersion := workspace.RequireVersions(filepath.Join(root, m.Dir, "go.mod"))[req]; reqVersion != "" && reqVersion != version {
				s.changes = append(s.changes, fmt.Sprintf("require %s %s -> %s", req, reqVersion, version))
			}
		}
		steps = append(steps, s)
	}
	return steps, nil
}

// tagOrder returns the modules in an order that a release can follow.
//
// The root module comes first. Every other module comes after the modules it
// requires, so a tag never points at a sibling that has no tag yet.
func tagOrder(mods []workspace.Module) ([]workspace.Module, error) {
	byDir := map[string]workspace.Module{}
	for _, m := range mods {
		byDir[m.Dir] = m
	}
	var root workspace.Module
	for _, m := range mods {
		if m.Dir == "." {
			root = m
		}
	}

	var order []workspace.Module
	done := map[string]bool{}
	if root.Path != "" {
		order = append(order, root)
		done[root.Dir] = true
	}
	for len(order) < len(mods) {
		progress := false
		for _, m := range mods {
			if done[m.Dir] {
				continue
			}
			ready := true
			for _, req := range m.Requires {
				for _, other := range mods {
					if other.Path == req && !done[other.Dir] {
						ready = false
					}
				}
			}
			if ready {
				order = append(order, m)
				done[m.Dir] = true
				progress = true
			}
		}
		if !progress {
			return nil, fmt.Errorf("the require graph holds a cycle")
		}
	}
	return order, nil
}

// tag returns the git tag of a module. The root module uses the plain version,
// and every other module puts its directory in front, which is the rule that the
// Go module proxy follows.
func tag(m workspace.Module, version string) string {
	if m.Dir == "." {
		return version
	}
	return strings.TrimPrefix(filepath.ToSlash(m.Dir), "./") + "/" + version
}

// oldVersion returns the plain version of the previous release, or an empty
// string when the repository has no release yet.
func oldVersion(old string) string {
	if old == "dev" {
		return ""
	}
	return old
}

// lastTag returns the tag of the previous release for a module, or an empty
// string when the repository never tagged that module. A module that the
// previous release did not ship has no API to break, so it must not be read
// through the proxy.
func lastTag(root string, m workspace.Module, old string) string {
	if old == "" || old == "dev" {
		return ""
	}
	name := tag(m, old)
	if !tagExists(root, name) {
		return ""
	}
	return name
}

// tagExists reports whether the repository holds the tag.
func tagExists(root, name string) bool {
	cmd := exec.CommandContext(context.Background(), "git", "-C", root, "rev-parse", "-q", "--verify", "refs/tags/"+name)
	return cmd.Run() == nil
}

// apiDiff compares the exported API of a module with its previous tag.
//
// apidiff reads a module through the build list of the working directory, so the
// previous tag needs its source on disk. The flow is three steps: export the old
// API from the old module's own directory, export the new API from the module
// directory, then compare the two files. A module without a previous tag has no
// API to break, and it says so.
func apiDiff(root string, s step) (string, error) {
	if s.lastTag == "" {
		return "no previous tag", nil
	}
	tmp, err := os.MkdirTemp("", "wlog-apidiff-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if out, err := goCommand(tmp, "mod", "init", "apidiff"); err != nil {
		return string(out), err
	}
	// The old API is exported from a worktree of the previous release. The
	// module's directory holds its siblings there, so the relative replace
	// directives of its go.mod resolve as they do in the repository. A
	// throwaway module or the module cache would leave the loader without the
	// siblings or the indirect dependencies.
	oldTree := filepath.Join(tmp, "old")
	if out, err := gitCommand(root, "worktree", "add", "--detach", oldTree, s.lastTag); err != nil {
		return string(out), err
	}
	defer func() {
		_, _ = gitCommand(root, "worktree", "remove", "--force", oldTree)
	}()

	oldExport := filepath.Join(tmp, "old.export")
	newExport := filepath.Join(tmp, "new.export")
	if out, err := goCommand(filepath.Join(oldTree, s.module.Dir), "run", apidiff+"@latest", "-m", "-w", oldExport, s.module.Path); err != nil {
		return string(out), err
	}
	if out, err := goCommand(filepath.Join(root, s.module.Dir), "run", apidiff+"@latest", "-m", "-w", newExport, s.module.Path); err != nil {
		return string(out), err
	}
	out, err := goCommand(tmp, "run", apidiff+"@latest", "-m", oldExport, newExport)
	return string(out), err
}

// goCommand runs one go command in a directory with GOWORK=off, so a throwaway
// module never joins the workspace.
func goCommand(dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// gitCommand runs one git command inside dir.
func gitCommand(dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// requireEdit reads a change line of the plan, such as
// "require example.com/lib v0.1.0 -> v0.2.0", and returns the module path. It
// reports false for any other line, so a later change to the plan cannot make
// apply edit the wrong module.
// requireVersion reads the version that a module's go.mod requires, or an empty
// string when the module requires no such path.
func requireVersion(root, dir, path string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "go", "mod", "edit", "-json")
	cmd.Dir = filepath.Join(root, dir)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	text, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: go mod edit -json: %w", dir, err)
	}
	var file struct {
		Require []struct {
			Path    string
			Version string
		}
	}
	if err := json.Unmarshal(text, &file); err != nil {
		return "", fmt.Errorf("%s: read go.mod: %w", dir, err)
	}
	for _, req := range file.Require {
		if req.Path == path {
			return req.Version, nil
		}
	}
	return "", nil
}

func requireEdit(change string) (string, bool) {
	fields := strings.Fields(change)
	if len(fields) != 5 || fields[0] != "require" || fields[3] != "->" {
		return "", false
	}
	return fields[1], true
}

// apply rewrites the require lines, commits them, and tags every module in order.
//
// The command updates tools/version.txt first, so the next release reads the
// version it just shipped.
func apply(root, version string, steps []step, out io.Writer) error {
	for _, s := range steps {
		for _, change := range s.changes {
			req, ok := requireEdit(change)
			if !ok {
				continue
			}
			args := []string{"mod", "edit", "-require=" + req + "@" + version}
			cmd := exec.CommandContext(context.Background(), "go", args...)
			cmd.Dir = filepath.Join(root, s.module.Dir)
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if text, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %w\n%s", s.module.Dir, err, text)
			}
			// Read the file back, so a release never tags a module whose require
			// edit did not land.
			got, err := requireVersion(root, s.module.Dir, req)
			if err != nil {
				return err
			}
			if got != version {
				return fmt.Errorf("%s: require %s stayed at %q, want %q, so the release stops before it tags",
					s.module.Dir, req, got, version)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, versionFile), []byte(version+"\n"), 0o600); err != nil {
		return err
	}

	commit := exec.CommandContext(context.Background(), "git", "add", "-A")
	commit.Dir = root
	if text, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, text)
	}
	commit = exec.CommandContext(context.Background(), "git", "commit", "-m", "chore(release): "+version)
	commit.Dir = root
	if text, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, text)
	}

	for _, s := range steps {
		name := tag(s.module, version)
		cmd := exec.CommandContext(context.Background(), "git", "tag", name)
		cmd.Dir = root
		if text, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git tag %s: %w\n%s", name, err, text)
		}
		say(out, "tagged "+name)
	}
	say(out, "\nthe tags are local. Push them with: git push origin --tags")
	return nil
}

// readVersion reads the version file.
func readVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// readLines reads at most count non-empty lines. The caller compares them with
// the plan, so a short list reports the same difference as a wrong one.
func readLines(in io.Reader, count int) ([]string, error) {
	scanner := bufio.NewScanner(in)
	var lines []string
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
		if len(lines) == count {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// oneLine returns the interesting lines of an apidiff report as one line, so the dry run
// shows every change. A line that names an ignored internal package carries no change.
func oneLine(report string) string {
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(report), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Ignoring ") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "no change"
	}
	return strings.Join(lines, "; ")
}
