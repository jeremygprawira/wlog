// Package wlogstdlog bridges the standard library logger, so a line written inside a unit
// of work folds into that unit's logs array.
//
// Read top to bottom: Logger returns a *log.Logger bound to one context, and its writer
// folds every line through core's AppendLog. ErrorLog returns the logger for
// http.Server.ErrorLog, and each of its lines becomes one plain event, because net/http
// writes it with no context.
//
// A global log.Printf never folds, even after slog.SetDefault, because a global call
// carries no context and reaches no event.
package wlogstdlog

import (
	"context"
	"log"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Logger returns the *log.Logger of one unit of work: every line it writes folds into the
// logs array of the event on ctx. Call it once per unit, because the returned logger holds
// the context. The prefix and the flags are the standard library ones.
func Logger(ctx context.Context, prefix string, flags int) *log.Logger {
	return log.New(&foldingWriter{ctx: ctx}, prefix, flags)
}

// foldingWriter folds each line into the event on its context.
type foldingWriter struct {
	ctx context.Context
}

// Write folds one line at level info, and it reports the length the logger expects. A
// write with no event reports a problem and never panics.
func (w *foldingWriter) Write(p []byte) (int, error) {
	wlog.AppendLog(w.ctx, wlog.LogLine{Level: "info", Msg: trimmed(p)})
	return len(p), nil
}

// ErrorLog returns the *log.Logger for http.Server.ErrorLog. Each line becomes one plain
// event at level error through l, because the server writes it with no context.
func ErrorLog(l *wlog.Logger) *log.Logger {
	if l == nil {
		l = wlog.Default()
	}
	return log.New(&plainWriter{log: l}, "", 0)
}

// plainWriter writes each line as one plain event.
type plainWriter struct {
	log *wlog.Logger
}

// Write writes one plain event at level error, and it reports the length the logger
// expects.
func (w *plainWriter) Write(p []byte) (int, error) {
	ctx := w.log.WithContext(context.Background())
	wlog.Log(ctx, wlog.LevelError, trimmed(p))
	return len(p), nil
}

// trimmed drops the newline that the standard library logger adds.
func trimmed(p []byte) string {
	return strings.TrimSuffix(string(p), "\n")
}
