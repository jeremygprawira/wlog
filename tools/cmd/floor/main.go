// Command floor runs the tests of every module with the oldest Go that the
// module claims in its go line.
//
// A module's go line is a promise: everything in it compiles and passes on that
// toolchain. The command tests that promise by running `go test ./...` with
// GOWORK=off and GOTOOLCHAIN=go<floor> in each module. The GOWORK=off run also
// proves that the module builds without the workspace, so a user who follows the
// tag gets the same result.
//
// Module directories given as arguments limit the run. With -libs the command
// also tests each module against newer library versions, which is what a user
// gets on the day after the release.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// runner runs the tests of one module at one floor.
type runner func(dir, floor string) ([]byte, error)

// main finds the workspace root, then checks the modules at their floors.
func main() {
	libs := flag.Bool("libs", false, "also test each module against newer library versions")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}

	run := runner(runFloor)
	if *libs {
		run = runFloorWithNewerLibs
	}
	if err := check(root, flag.Args(), run, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "floor:", err)
	os.Exit(1)
}

// check runs every selected module at its floor and prints one problem per
// module that fails.
//
// An empty dirs list selects every module. A dir that names no module is an
// error, so a typo never turns into a silent pass. run is a parameter, so a test
// proves the selection, the floor lookup, and the report without a toolchain.
func check(root string, dirs []string, run runner, out io.Writer) error {
	mods, err := workspace.Modules(root)
	if err != nil {
		return err
	}
	selected, err := selectModules(mods, dirs)
	if err != nil {
		return err
	}

	bad := 0
	for _, m := range selected {
		dir := filepath.Join(root, m.Dir)
		text, err := run(dir, m.Floor)
		if err == nil {
			continue
		}
		bad++
		if _, err := fmt.Fprintf(out, "%s: FLOOR1: tests fail at Go %s\n%s", rel(root, dir), m.Floor, text); err != nil {
			return err
		}
		if len(text) > 0 && text[len(text)-1] != '\n' {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d module(s) fail at their floor", bad)
	}
	return nil
}

// selectModules returns the modules that the dirs arguments name, or every
// module when dirs is empty.
func selectModules(mods []workspace.Module, dirs []string) ([]workspace.Module, error) {
	if len(dirs) == 0 {
		return mods, nil
	}
	var selected []workspace.Module
	for _, want := range dirs {
		want = "./" + strings.TrimPrefix(filepath.ToSlash(want), "./")
		if want == "./." {
			want = "."
		}
		found := false
		for _, m := range mods {
			if m.Dir == want {
				selected = append(selected, m)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no workspace module at %s", want)
		}
	}
	return selected, nil
}

// runFloor runs `go test ./...` in dir with GOWORK=off and the floor toolchain.
func runFloor(dir, floor string) ([]byte, error) {
	args := append([]string{"test"}, linkArgs()...)
	args = append(args, "./...")
	return runGo(dir, floor, args)
}

// runFloorWithNewerLibs runs the floor test twice: once as it stands, and once
// with every dependency upgraded. The second run shows what a user gets from a
// newer library, without waiting for a release.
//
// It upgrades in place and restores go.mod and go.sum afterwards, so a check
// never leaves the tree changed.
func runFloorWithNewerLibs(dir, floor string) ([]byte, error) {
	if text, err := runFloor(dir, floor); err != nil {
		return text, err
	}

	gomod, gosum, err := snapshot(dir)
	if err != nil {
		return nil, err
	}
	defer restore(dir, gomod, gosum)

	if text, err := runGo(dir, floor, []string{"get", "-u", "./..."}); err != nil {
		return text, err
	}
	return runFloor(dir, floor)
}

// snapshot reads go.mod and go.sum, so a later upgrade can be undone.
func snapshot(dir string) (gomod, gosum []byte, err error) {
	gomod, err = os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil, nil, err
	}
	gosum, err = os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	return gomod, gosum, nil
}

// restore writes the two files back.
func restore(dir string, gomod, gosum []byte) {
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), gomod, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "floor: restore go.mod:", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), gosum, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "floor: restore go.sum:", err)
	}
}

// runGo runs one go command in dir with GOWORK=off and the floor toolchain.
func runGo(dir, floor string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN="+toolchain(floor))
	return cmd.CombinedOutput()
}

// toolchain returns the GOTOOLCHAIN value of a floor.
//
// A floor without a patch version names a language version, such as "1.21", and
// the go command needs a full toolchain version. The first release of that
// language version is the floor, so "1.21" becomes "go1.21.0".
func toolchain(floor string) string {
	if strings.Count(floor, ".") < 2 {
		return "go" + floor + ".0"
	}
	return "go" + floor
}

// linkArgs returns the link flags that this platform needs to run a test binary
// of an old toolchain.
//
// macOS 26 refuses a binary that has no LC_UUID load command, and the internal
// linker of Go 1.21 and Go 1.22 leaves it out. External linking adds it back.
// Linux and the newer toolchains need nothing.
func linkArgs() []string {
	if runtime.GOOS == "darwin" {
		return []string{"-ldflags=-linkmode=external"}
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
