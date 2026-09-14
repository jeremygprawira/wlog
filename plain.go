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

func plainLog(ctx context.Context, level Level, msg string, kv []any) {
	l := loggerFrom(ctx)
	if l == nil {
		return
	}
	if levelRank[level] < levelRank[l.minLevel] {
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
	maps.Copy(out, kvToMap(kv))

	l.pipeline(ctx, out)
}
