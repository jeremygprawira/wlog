// This file holds the input direction: the hook and the bound writer that fold zerolog
// lines into the open event.
package wlogzerolog

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/rs/zerolog"

	"github.com/jeremygprawira/wlog"
)

// Hook returns a zerolog hook that folds the level and the message of an event that
// carries a context, and discards the event so no writer sees it.
//
// A hook cannot read the fields of an event, so use Plugin or Bind for a logger whose
// fields fold too.
func Hook() zerolog.Hook { return hook{} }

// hook folds the events that carry an open event.
type hook struct{}

// Run folds one event and discards it.
func (hook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	ctx := e.GetCtx()
	if ctx == nil || !wlog.HasEvent(ctx) {
		return
	}
	wlog.AppendLog(ctx, wlog.LogLine{Level: levelName(level), Msg: msg})
	e.Discard()
}

// Plugin returns a plugin that binds a logger to the context of every unit of work, so
// zerolog.Ctx(ctx) returns it and its lines fold, fields included. A debug line folds
// only when the base logger allows debug, because zerolog filters before the writer.
func Plugin(base zerolog.Logger) wlog.Plugin { return plugin{base: base} }

// plugin binds a logger into the event context.
type plugin struct {
	base zerolog.Logger
}

// Name names the plugin.
func (plugin) Name() string { return "wlogzerolog" }

// OnStart binds base to the context of one new unit of work.
func (p plugin) OnStart(ctx context.Context, _ string) context.Context {
	return Bind(ctx, p.base).WithContext(ctx)
}

// Bind returns a logger whose lines fold into the open event of ctx, fields included.
// With no event the lines reach the writer of base.
func Bind(ctx context.Context, base zerolog.Logger) zerolog.Logger {
	return base.Output(&writer{ctx: ctx})
}

// writer folds each JSON line of zerolog into the event of one context.
type writer struct {
	ctx context.Context
}

// Write decodes one line and folds it. A line that does not parse is dropped, and the
// line itself never reaches another writer.
func (w *writer) Write(p []byte) (int, error) {
	if !wlog.HasEvent(w.ctx) {
		return len(p), nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(p), &decoded); err != nil {
		return len(p), nil
	}
	level, _ := decoded["level"].(string)
	message, _ := decoded["message"].(string)
	delete(decoded, "level")
	delete(decoded, "message")
	delete(decoded, "time")
	line := wlog.LogLine{Level: wlogLevel(level), Msg: message}
	if len(decoded) > 0 {
		line.Attrs = decoded
	}
	wlog.AppendLog(w.ctx, line)
	return len(p), nil
}

// levelName maps one zerolog level constant to the level of a folded line, never through
// Level.String(), because the level names are package globals an app can change.
func levelName(level zerolog.Level) string {
	switch level {
	case zerolog.TraceLevel, zerolog.DebugLevel:
		return string(wlog.LevelDebug)
	case zerolog.WarnLevel:
		return string(wlog.LevelWarn)
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		return string(wlog.LevelError)
	default:
		return string(wlog.LevelInfo)
	}
}

// wlogLevel maps one level name of a JSON line to the level of a folded line.
func wlogLevel(name string) string {
	switch name {
	case "trace", "debug":
		return string(wlog.LevelDebug)
	case "warn":
		return string(wlog.LevelWarn)
	case "error", "fatal", "panic":
		return string(wlog.LevelError)
	default:
		return string(wlog.LevelInfo)
	}
}
