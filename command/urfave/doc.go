// Package wlogurfave is wlog's urfave/cli v3 adapter: one event per command run, with the
// command path, the flags that were set, and the exit code.
//
// urfave exits inside Run for an exit-coder error, and os.Exit runs no defer, so Run sets the
// root ExitErrHandler to record and flush before the exit.
//
// Read top to bottom: Run runs a command and returns the code for os.Exit.
// The setup line lives in docs/async-adapters.md, which `make snippets` compiles.
package wlogurfave
