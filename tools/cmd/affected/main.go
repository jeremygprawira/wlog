// Command affected lists the modules that a diff touches, plus their dependents.
//
// It prints one workspace dir per line, so a CI job can build a test matrix from
// the output. The diff comes from git diff --name-only for the base ref, so both
// committed and uncommitted changes count.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// main prints the affected dirs, or exits 1 with the error on stderr.
func main() {
	base := flag.String("base", "HEAD~1", "git ref to compare against")
	flag.Parse()

	if err := run(*base); err != nil {
		fmt.Fprintln(os.Stderr, "affected:", err)
		os.Exit(1)
	}
}

// run writes the affected module dirs of the working directory to stdout.
func run(base string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	dirs, err := workspace.Affected(root, base)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if _, err := fmt.Fprintln(os.Stdout, dir); err != nil {
			return err
		}
	}
	return nil
}
