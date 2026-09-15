// Package wlogslog folds wlog events into the standard library's log/slog, and folds
// slog records back into a wlog event. It ships in the root module, since log/slog is
// standard library.
//
// Output: Drain writes each wlog event as one slog record. Input: Handler appends a
// record made inside an event to that event's logs[].
package wlogslog

import (
	"context"
	"log/slog"
	"time"

	"github.com/jeremygprawira/wlog"
)

// Drain returns a wlog.Drain that writes each event as one slog record through handler.
// The event's operation becomes the record message and its level becomes the record
// level. Every other field becomes an attribute, and a nested map stays an slog group
// rather than a flattened key.
func Drain(handler slog.Handler) wlog.Drain {
	return wlog.DrainFunc(func(ctx context.Context, event map[string]any) {
		rec := slog.NewRecord(recordTime(event), slogLevel(event["level"]), eventMessage(event), 0)
		rec.AddAttrs(eventAttrs(event)...)
		_ = handler.Handle(ctx, rec)
	})
}

// recordTime uses the event's own timestamp when it parses, so a drain that runs later
// (a batch flush, a retry) still reports the moment the event was emitted.
func recordTime(event map[string]any) time.Time {
	if s, ok := event["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	return time.Now()
}

// slogLevel maps SPEC-core's Level string to slog's own level. Anything unrecognized
// (or missing) is INFO, matching core's default level.
func slogLevel(v any) slog.Level {
	switch v {
	case string(wlog.LevelDebug):
		return slog.LevelDebug
	case string(wlog.LevelWarn):
		return slog.LevelWarn
	case string(wlog.LevelError):
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

// eventMessage picks the record message: a wide event's operation, or a plain log
// line's message when there is no operation.
func eventMessage(event map[string]any) string {
	if op := stringValue(event["operation"]); op != "" {
		return op
	}
	return stringValue(event["message"])
}

// eventAttrs turns every field except the ones already carried by the record
// (timestamp, level, operation, message) into an slog attribute.
func eventAttrs(event map[string]any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(event))
	for k, v := range event {
		switch k {
		case "timestamp", "level", "operation", "message":
			continue
		}
		attrs = append(attrs, attrFor(k, v))
	}
	return attrs
}

// attrFor renders one value: a map becomes an slog group, so nesting survives, and
// everything else (including arrays) passes through slog.Any.
func attrFor(key string, v any) slog.Attr {
	if nested, ok := v.(map[string]any); ok {
		return slog.Group(key, groupArgs(nested)...)
	}
	return slog.Any(key, v)
}

func groupArgs(m map[string]any) []any {
	args := make([]any, 0, len(m))
	for k, v := range m {
		args = append(args, attrFor(k, v))
	}
	return args
}
