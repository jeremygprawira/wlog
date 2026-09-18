package wlog

import (
	"context"
	"maps"
	"time"
)

// Info, Warn, and Debug write one standalone line for a message outside any wide
// event — a startup or shutdown message, or anything that doesn't belong to a unit of
// work Start began. kv are key, value, ... pairs merged into the line, same shape as
// SetGroup's arguments.
//
// Called inside a request's context, the line is still its own standalone event, not
// folded into the request's event — an input adapter (log-slog and friends) is what
// merges plain calls into the current wide event, not this package-level function.
func Info(ctx context.Context, msg string, kv ...any)  { plainLog(ctx, LevelInfo, msg, kv) }
func Warn(ctx context.Context, msg string, kv ...any)  { plainLog(ctx, LevelWarn, msg, kv) }
func Debug(ctx context.Context, msg string, kv ...any) { plainLog(ctx, LevelDebug, msg, kv) }

// Log writes one standalone line at any level, including error. Info, Warn, and Debug
// leave a line below the minimum level of the Logger out, and Log writes it.
func Log(ctx context.Context, level Level, msg string, kv ...any) {
	plainLine(ctx, level, msg, kvToMap(kv), true, nil)
}

func plainLog(ctx context.Context, level Level, msg string, kv []any) {
	plainLine(ctx, level, msg, kvToMap(kv), false, nil)
}

// plainLine writes one standalone line: a log event with the message and the pairs. A
// level below the minimum of the Logger is dropped, unless anyLevel is set. info carries
// the ErrorInfo of a line that recorded an error outside any unit of work.
func plainLine(ctx context.Context, level Level, msg string, pairs map[string]any, anyLevel bool, info *ErrorInfo) {
	l := loggerFrom(ctx)
	if l == nil {
		l = Default()
	}
	if !l.Enabled() {
		l.dropEvent(dropDisabled)
		return
	}
	if !anyLevel && levelRank[level] < levelRank[l.minLevel] {
		l.dropEvent(dropLevel)
		return
	}

	out := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     string(level),
		"kind":      "log",
		"message":   msg,
	}
	if l.service != (serviceInfo{}) {
		out["service"] = map[string]any{
			"name": l.service.name, "version": l.service.version, "env": l.service.env,
		}
	}
	// The pairs are copied into the tree wlog owns before the redactor runs, so a
	// struct renders through its json tags and a caller's map is never stored.
	maps.Copy(out, copyMap(pairs, 1))
	// An error that arrived with no event is normalized the same way emit does it, so
	// redaction walks inside its detail.
	if info != nil {
		out["error"] = normalize(*info)
	}

	l.pipeline(ctx, out)
}
