// Package wloghclog folds hashicorp/go-hclog lines into the open wlog event, and writes
// wlog events through an hclog logger.
//
// Read top to bottom: Plugin binds a logger to the context of each unit of work with
// hclog.WithContext, so hclog.FromContext returns it for the rest of the unit. The bound
// logger folds every level call, keeps its name and its With pairs, and answers every
// level check true while the event is open. Drain writes each event as one hclog record.
//
// This is the whole setup:
//
//	log := wlog.New(wlog.WithPlugins(wloghclog.Plugin(base)))
package wloghclog

import (
	"context"
	"io"
	"log"
	"strings"

	"github.com/hashicorp/go-hclog"

	"github.com/jeremygprawira/wlog"
)

// Plugin returns a plugin that binds base to the context of every new unit of work. A
// nil base becomes hclog's null logger.
func Plugin(base hclog.Logger) wlog.Plugin {
	if base == nil {
		base = hclog.NewNullLogger()
	}
	return plugin{base: base}
}

// plugin binds a logger into the event context.
type plugin struct {
	base hclog.Logger
}

// Name names the plugin.
func (plugin) Name() string { return "wloghclog" }

// OnStart binds base to the context of one new unit of work.
func (p plugin) OnStart(ctx context.Context, _ string) context.Context {
	return hclog.WithContext(ctx, Bind(ctx, p.base))
}

// Bind returns a logger bound to ctx. hclog.FromContext returns it, and its calls fold
// into the open event of that context. With no event, its calls reach base.
func Bind(ctx context.Context, base hclog.Logger) hclog.Logger {
	if base == nil {
		base = hclog.NewNullLogger()
	}
	return &logger{ctx: ctx, base: base}
}

// logger folds the calls that carry an open event and forwards the rest.
type logger struct {
	ctx  context.Context
	base hclog.Logger
}

// Log folds one message, and it forwards the call when the event is gone.
func (l *logger) Log(level hclog.Level, msg string, args ...any) {
	if !wlog.HasEvent(l.ctx) {
		l.base.Log(level, msg, args...)
		return
	}
	wlog.AppendLog(l.ctx, l.line(level, msg, args))
}

// Trace folds one trace line.
func (l *logger) Trace(msg string, args ...any) { l.Log(hclog.Trace, msg, args...) }

// Debug folds one debug line.
func (l *logger) Debug(msg string, args ...any) { l.Log(hclog.Debug, msg, args...) }

// Info folds one info line.
func (l *logger) Info(msg string, args ...any) { l.Log(hclog.Info, msg, args...) }

// Warn folds one warn line.
func (l *logger) Warn(msg string, args ...any) { l.Log(hclog.Warn, msg, args...) }

// Error folds one error line.
func (l *logger) Error(msg string, args ...any) { l.Log(hclog.Error, msg, args...) }

// IsTrace reports true while the event is open, so an app never elides a folded line.
func (l *logger) IsTrace() bool { return wlog.HasEvent(l.ctx) || l.base.IsTrace() }

// IsDebug reports true while the event is open.
func (l *logger) IsDebug() bool { return wlog.HasEvent(l.ctx) || l.base.IsDebug() }

// IsInfo reports true while the event is open.
func (l *logger) IsInfo() bool { return wlog.HasEvent(l.ctx) || l.base.IsInfo() }

// IsWarn reports true while the event is open.
func (l *logger) IsWarn() bool { return wlog.HasEvent(l.ctx) || l.base.IsWarn() }

// IsError reports true while the event is open.
func (l *logger) IsError() bool { return wlog.HasEvent(l.ctx) || l.base.IsError() }

// ImpliedArgs returns the pairs of the wrapped logger.
func (l *logger) ImpliedArgs() []any { return l.base.ImpliedArgs() }

// With keeps the context and adds the pairs to the wrapped logger.
func (l *logger) With(args ...any) hclog.Logger {
	return &logger{ctx: l.ctx, base: l.base.With(args...)}
}

// Name returns the name of the wrapped logger.
func (l *logger) Name() string { return l.base.Name() }

// Named keeps the context and names the wrapped logger.
func (l *logger) Named(name string) hclog.Logger {
	return &logger{ctx: l.ctx, base: l.base.Named(name)}
}

// ResetNamed keeps the context and renames the wrapped logger.
func (l *logger) ResetNamed(name string) hclog.Logger {
	return &logger{ctx: l.ctx, base: l.base.ResetNamed(name)}
}

// SetLevel sets the level of the wrapped logger.
func (l *logger) SetLevel(level hclog.Level) { l.base.SetLevel(level) }

// GetLevel returns the level of the wrapped logger.
func (l *logger) GetLevel() hclog.Level { return l.base.GetLevel() }

// StandardLogger returns the standard logger of the wrapped logger.
func (l *logger) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	return l.base.StandardLogger(opts)
}

// StandardWriter returns the standard writer of the wrapped logger.
func (l *logger) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return l.base.StandardWriter(opts)
}

// line builds one folded line from the name, the implied pairs, and the call pairs.
func (l *logger) line(level hclog.Level, msg string, args []any) wlog.LogLine {
	line := wlog.LogLine{Level: strings.ToLower(level.String()), Msg: msg}
	attrs := map[string]any{}
	addPairs(attrs, l.base.ImpliedArgs())
	addPairs(attrs, args)
	if name := l.base.Name(); name != "" {
		attrs["logger"] = name
	}
	if len(attrs) > 0 {
		line.Attrs = attrs
	}
	return line
}

// addPairs writes the string keys of one argument list into a map, and it skips a
// trailing key with no value.
func addPairs(into map[string]any, args []any) {
	for i := 0; i+1 < len(args); i += 2 {
		if key, ok := args[i].(string); ok {
			into[key] = args[i+1]
		}
	}
}

// Drain returns a wlog.Drain that writes each event as one hclog record. The event's
// operation becomes the message, and the rest of the event becomes the pairs. The level
// key is the level of the record, so it never becomes a pair.
func Drain(l hclog.Logger) wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		pairs := make([]any, 0, len(event)*2)
		for key, value := range event {
			switch key {
			case "level", "operation", "message":
				continue
			}
			pairs = append(pairs, key, value)
		}
		l.Log(hclogLevel(event["level"]), eventMessage(event), pairs...)
	})
}

// hclogLevel maps SPEC-core's Level string to an hclog level, defaulting to info.
func hclogLevel(v any) hclog.Level {
	switch v {
	case string(wlog.LevelDebug):
		return hclog.Debug
	case string(wlog.LevelWarn):
		return hclog.Warn
	case string(wlog.LevelError):
		return hclog.Error
	default:
		return hclog.Info
	}
}

// eventMessage picks the record message: a wide event's operation, or a plain line's
// message when there is no operation.
func eventMessage(event map[string]any) string {
	if op, _ := event["operation"].(string); op != "" {
		return op
	}
	msg, _ := event["message"].(string)
	return msg
}
