// This file holds the input direction: the hook and the formatter that fold a logrus
// entry carrying a context into the open event.
package wloglogrus

import (
	"github.com/sirupsen/logrus"

	"github.com/jeremygprawira/wlog"
)

// foldedKey marks the entry a hook folded, so the formatter writes no line for it.
const foldedKey = "wlog.folded"

// Install adds the wlog hook to one logger and wraps its formatter, so an entry whose
// Context holds an open event folds into that event and writes no line.
func Install(logger *logrus.Logger) {
	next := logger.Formatter
	if next == nil {
		next = &logrus.TextFormatter{}
	}
	logger.Formatter = &formatter{next: next}
	logger.AddHook(&hook{})
}

// hook folds the entries that carry an open event.
type hook struct{}

// Levels covers every level, so every entry reaches Fire.
func (h *hook) Levels() []logrus.Level { return logrus.AllLevels }

// Fire folds one entry, and it marks the entry so the formatter stays silent. A hook
// cannot stop logrus from writing, so the marker does that job.
func (h *hook) Fire(entry *logrus.Entry) error {
	if entry.Context == nil || !wlog.HasEvent(entry.Context) {
		return nil
	}
	wlog.AppendLog(entry.Context, line(entry))
	entry.Data[foldedKey] = true
	return nil
}

// formatter writes no bytes for a folded entry, and it defers to the wrapped formatter
// for every other entry.
type formatter struct {
	next logrus.Formatter
}

// Format returns no bytes for a folded entry.
func (f *formatter) Format(entry *logrus.Entry) ([]byte, error) {
	if folded, _ := entry.Data[foldedKey].(bool); folded {
		return nil, nil
	}
	return f.next.Format(entry)
}

// line builds one folded line from one entry and its fields.
func line(entry *logrus.Entry) wlog.LogLine {
	line := wlog.LogLine{Level: levelName(entry.Level), Msg: entry.Message}
	if len(entry.Data) > 0 {
		attrs := make(map[string]any, len(entry.Data))
		for key, value := range entry.Data {
			attrs[key] = value
		}
		line.Attrs = attrs
	}
	return line
}

// levelName maps one logrus level to the level of a folded line: warning becomes warn,
// trace becomes debug, and panic and fatal become error.
func levelName(level logrus.Level) string {
	switch level {
	case logrus.TraceLevel, logrus.DebugLevel:
		return string(wlog.LevelDebug)
	case logrus.WarnLevel:
		return string(wlog.LevelWarn)
	case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
		return string(wlog.LevelError)
	default:
		return string(wlog.LevelInfo)
	}
}
