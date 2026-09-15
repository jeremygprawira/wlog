// Package wlogzap writes wlog events to a zap logger, so a team already on zap can add
// wlog without changing where its logs land.
package wlogzap

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jeremygprawira/wlog"
)

// Drain returns a wlog.Drain that writes each event as one zap entry through logger.
// The event's operation (or a plain line's message) becomes the entry message and its
// level becomes the zap level. Every other field becomes a field, and a nested map
// stays an object rather than a flattened key.
func Drain(logger *zap.Logger) wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		fields := make([]zap.Field, 0, len(event))
		for k, v := range event {
			switch k {
			case "level", "operation", "message":
				continue
			}
			fields = append(fields, zap.Any(k, v))
		}
		logger.Log(zapLevel(event["level"]), eventMessage(event), fields...)
	})
}

// zapLevel maps SPEC-core's Level string to zap's own level, defaulting to info.
func zapLevel(v any) zapcore.Level {
	switch v {
	case string(wlog.LevelDebug):
		return zap.DebugLevel
	case string(wlog.LevelWarn):
		return zap.WarnLevel
	case string(wlog.LevelError):
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}

// eventMessage picks the entry message: a wide event's operation, or a plain log
// line's message when there is no operation.
func eventMessage(event map[string]any) string {
	if op, _ := event["operation"].(string); op != "" {
		return op
	}
	msg, _ := event["message"].(string)
	return msg
}
