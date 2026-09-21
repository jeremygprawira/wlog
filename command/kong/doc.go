// Package wlogkong is wlog's kong adapter: one event per command run, with the command path,
// the flags that were set, and the exit code.
//
// kong exits inside Parse for a usage fault and for --help, and os.Exit runs no defer, so Run
// passes kong.Exit a function that records the exit code and flushes first.
//
// Read top to bottom: Run parses a grammar, runs the selected command, and returns the code for
// os.Exit.
package wlogkong
