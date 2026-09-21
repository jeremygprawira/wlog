// Package wlogcobra is wlog's cobra adapter: one event per command run, with the command
// path, the flags that were set, and the exit code.
//
// A cobra post hook never runs after a failure, so the event ends at the top: Execute runs
// the command tree, records the outcome, and flushes the drains before the process exits.
//
// Read top to bottom: Execute runs a command tree and returns the code for os.Exit.
package wlogcobra
