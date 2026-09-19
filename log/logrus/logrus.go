// Package wloglogrus writes wlog events to a logrus logger, so a team already on
// logrus can add wlog without changing where its logs land.
package wloglogrus

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/jeremygprawira/wlog"
)

// Drain returns a wlog.Drain that writes each event as one logrus entry through
// logger. The event's operation (or a plain line's message) becomes the entry message
// and its level becomes the logrus level. A nested map stays one field value, which a
// JSON formatter renders as an object.
func Drain(logger *logrus.Logger) wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		fields := logrus.Fields{}
		for k, v := range event {
			switch k {
			case "level", "operation", "message":
				continue
			}
			fields[k] = v
		}
		logger.WithFields(fields).Log(logrusLevel(event["level"]), eventMessage(event))
	})
}

// logrusLevel maps SPEC-core's Level string to logrus's own level, defaulting to info.
func logrusLevel(v any) logrus.Level {
	switch v {
	case string(wlog.LevelDebug):
		return logrus.DebugLevel
	case string(wlog.LevelWarn):
		return logrus.WarnLevel
	case string(wlog.LevelError):
		return logrus.ErrorLevel
	default:
		return logrus.InfoLevel
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
