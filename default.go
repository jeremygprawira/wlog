// This file holds the one piece of package-level state wlog allows: the default Logger.
// A package function reads the Logger on the context. An empty context falls back to this
// pointer, so a program logs with no setup at all.
package wlog

import "sync/atomic"

// defaultLogger is the Logger a package function uses when the context carries none. It
// holds no per-event data, and CLAUDE.md names it as the one allowed piece of package
// state.
var defaultLogger atomic.Pointer[Logger]

// SetDefault sets the Logger a package function uses when the context carries none. A nil
// Logger is ignored, so Default stays usable.
func SetDefault(l *Logger) {
	if l != nil {
		defaultLogger.Store(l)
	}
}

// Default returns the default Logger. It builds one with New on first use, so it is never
// nil and a caller may always use it.
func Default() *Logger {
	if l := defaultLogger.Load(); l != nil {
		return l
	}
	l := New()
	if defaultLogger.CompareAndSwap(nil, l) {
		return l
	}
	// Another goroutine won the race, and its Logger is the one every caller sees.
	return defaultLogger.Load()
}
