// Package wlogzerolog writes wlog events to a zerolog logger, so a team already on
// zerolog can add wlog without changing where its logs land.
package wlogzerolog

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/jeremygprawira/wlog"
)

// Drain returns a wlog.Drain that writes each event as one zerolog record through
// logger. The event's operation (or a plain line's message) becomes the record
// message and its level becomes the zerolog level. Every other field becomes a field,
// and a nested map stays an object rather than a flattened key.
func Drain(logger zerolog.Logger) wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		ev := logger.WithLevel(zerologLevel(event["level"]))
		for k, v := range event {
			switch k {
			case "level", "operation", "message":
				continue
			}
			ev = ev.Interface(k, v)
		}
		ev.Msg(eventMessage(event))
	})
}

// zerologLevel maps SPEC-core's Level string to zerolog's own level, defaulting to info.
func zerologLevel(v any) zerolog.Level {
	switch v {
	case string(wlog.LevelDebug):
		return zerolog.DebugLevel
	case string(wlog.LevelWarn):
		return zerolog.WarnLevel
	case string(wlog.LevelError):
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// eventMessage picks the record message: a wide event's operation, or a plain log
// line's message when there is no operation.
func eventMessage(event map[string]any) string {
	if op, _ := event["operation"].(string); op != "" {
		return op
	}
	msg, _ := event["message"].(string)
	return msg
}
