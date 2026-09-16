// Command modules prints every module of the go.work workspace as JSON.
//
// Every other tools command reads this list, so the command is the single place
// that decides what a module is. It prints the module path, the workspace dir,
// the Go floor, the required modules, and the modules that depend on it.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jeremygprawira/wlog/tools/internal/workspace"
)

// main prints the workspace JSON, or exits 1 with the error on stderr.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "modules:", err)
		os.Exit(1)
	}
}

// run reads the workspace of the working directory and writes it to stdout.
func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	mods, err := workspace.Modules(root)
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(mods, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(out))
	return err
}
