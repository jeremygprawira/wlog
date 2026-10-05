// Command vuln looks for known vulnerabilities in the dependency set that a user
// gets.
//
// A module keeps the lowest version of a library that its API needs, so a scan
// of the repository alone finds only the vulnerabilities of that old version.
// The command instead upgrades each module to the newest libraries, scans that
// copy, and puts the module files back. The result is what a user meets on the
// day after the release.
//
// The upgrade happens in place, because a copy of a module loses the relative
// replace lines that point at its siblings. The command reads go.mod and go.sum
// first and writes them back after the scan, whatever the outcome.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jeremygprawira/wlog/tools/internal/tmpcopy"
	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// scanner is the command that looks for vulnerabilities.
const scanner = "golang.org/x/vuln/cmd/govulncheck"

// main finds the workspace root, then scans every module.
func main() {
	only := flag.String("only", "", "scan only the modules whose dir holds this string")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	root, err := workspace.FindRoot(wd)
	if err != nil {
		fail(err)
	}
	scannerPath, err := buildScanner(root)
	if err != nil {
		fail(err)
	}
	if err := check(root, *only, func(dir string) ([]byte, error) {
		return runVuln(dir, scannerPath)
	}, os.Stdout); err != nil {
		fail(err)
	}
}

// fail prints the error on stderr and exits 1.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "vuln:", err)
	os.Exit(1)
}

// check upgrades every module, scans it, and puts the module files back.
//
// It prints one line per module that the scanner rejects, with the finding
// itself. scan is a parameter, so a test proves the module selection and the
// report without a network call.
func check(root, only string, scan func(dir string) ([]byte, error), out io.Writer) error {
	mods, err := workspace.Modules(root)
	if err != nil {
		return err
	}

	// The upgrade runs on a copy of the whole workspace, so a killed run never leaves an
	// upgraded go.mod in the tree, and the module's tests still read their siblings.
	tmpRoot, err := tmpcopy.Workspace(root)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()

	bad := 0
	for _, m := range mods {
		if only != "" && !strings.Contains(m.Dir, only) {
			continue
		}
		dir := filepath.Join(tmpRoot, m.Dir)
		text, err := scan(dir)
		if err == nil {
			continue
		}
		bad++
		if _, err := fmt.Fprintf(out, "%s: VULN1: %v\n%s", m.Dir, err, indent(text)); err != nil {
			return err
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d module(s) hold a known vulnerability", bad)
	}
	return nil
}

// buildScanner builds govulncheck from this module and returns the path of the
// binary.
//
// The scanner must run against a module that does not require it, so a
// `go run` inside that module fails. One build here serves every module of the
// workspace, and each scan then needs no network.
func buildScanner(root string) (string, error) {
	dir, err := os.MkdirTemp("", "wlog-vuln-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "govulncheck")
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", path, scanner)
	cmd.Dir = filepath.Join(root, "tools")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build the scanner: %w\n%s", err, out)
	}
	return path, nil
}

// runVuln upgrades one module and scans it with the scanner binary.
//
// The upgrade writes go.mod and go.sum, which the caller puts back afterwards.
func runVuln(dir, scannerPath string) ([]byte, error) {
	// The upgrade runs inside the copy that the caller made, never in the tree.
	upgrade := exec.CommandContext(context.Background(), "go", "get", "-u", "./...")
	upgrade.Dir = dir
	upgrade.Env = append(os.Environ(), "GOWORK=off")
	if out, err := upgrade.CombinedOutput(); err != nil {
		return out, fmt.Errorf("upgrade: %w", err)
	}

	scan := exec.CommandContext(context.Background(), scannerPath, "./...")
	scan.Dir = dir
	scan.Env = append(os.Environ(), "GOWORK=off")
	out, err := scan.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("govulncheck found a vulnerability")
	}
	return out, nil
}

// indent puts four spaces before every line of a scanner report.
func indent(text []byte) string {
	lines := strings.Split(strings.TrimRight(string(text), "\n"), "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}
