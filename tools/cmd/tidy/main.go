// Command tidy checks that every workspace module is tidy with GOWORK=off.
//
// A module is tidy when `go mod tidy -diff` prints nothing, so the recipe a user
// gets from the tag needs no extra edit. The command runs the check per module
// and fails on the first module that is not tidy.
//
// With -check=false the command writes instead: it runs `go mod tidy` in each
// module, so a repair is one command.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// main finds the workspace root, then checks or repairs every module.
func main() {
	onlyCheck := flag.Bool("check", true, "only check and never write go.mod")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}

	if *onlyCheck {
		if err := check(root, runTidyDiff, os.Stdout); err != nil {
			fail(err)
		}
		return
	}
	if err := fix(root); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "tidy:", err)
	os.Exit(1)
}

// runTidyDiff runs `go mod tidy -diff` with GOWORK=off in one module dir.
//
// The go command exits 1 when it prints a diff, so a caller must read the
// output before it treats the error as a failure.
func runTidyDiff(dir string) ([]byte, error) {
	return runTidy(dir, "-diff")
}

// runTidy runs `go mod tidy` with extra args and GOWORK=off in one module dir.
func runTidy(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", append([]string{"mod", "tidy"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	return cmd.CombinedOutput()
}

// check runs the diff function over every module and prints one problem per
// module that is not tidy. It returns an error when it finds at least one.
//
// The diff function is a parameter, so a test proves both outcomes without a
// network call and without a go command.
func check(root string, diff func(dir string) ([]byte, error), out io.Writer) error {
	mods, err := workspace.Modules(root)
	if err != nil {
		return err
	}

	bad := 0
	for _, m := range mods {
		dir := filepath.Join(root, m.Dir)
		text, err := diff(dir)
		if err != nil && len(bytes.TrimSpace(text)) == 0 {
			return fmt.Errorf("%s: %w", m.Dir, err)
		}
		if len(bytes.TrimSpace(text)) == 0 {
			continue
		}
		bad++
		fmt.Fprintf(out, "%s: TIDY1: go mod tidy would change this module\n%s", rel(root, dir), text)
	}
	if bad > 0 {
		return fmt.Errorf("%d module(s) are not tidy", bad)
	}
	return nil
}

// fix runs `go mod tidy` in every module and stops at the first failure.
func fix(root string) error {
	mods, err := workspace.Modules(root)
	if err != nil {
		return err
	}
	for _, m := range mods {
		dir := filepath.Join(root, m.Dir)
		if out, err := runTidy(dir); err != nil {
			return fmt.Errorf("%s: %w\n%s", m.Dir, err, out)
		}
	}
	return nil
}

// rel returns path as "./x" relative to root.
func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return "./" + filepath.ToSlash(r)
}
