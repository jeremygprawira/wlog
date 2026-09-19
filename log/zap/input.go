// This file holds the input direction: the zap core that folds an entry carrying a
// context into the open event.
package wlogzap

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jeremygprawira/wlog"
)

// contextField names the field that Bind writes. Core marks an entry by the value and not
// by the name, so an app with another convention still folds.
const contextField = "ctx"

// Bind returns a logger that carries ctx as a field, so an entry logged through it folds
// into the open event of that context through Core.
func Bind(ctx context.Context, logger *zap.Logger) *zap.Logger {
	return logger.With(zap.Any(contextField, ctx))
}

// Core wraps next so an entry whose field holds a context.Context folds into the open
// event of that context. The context field never reaches next, and never reaches a sink.
//
// For an entry with no event, Write asks next for a check again and then writes, so a
// sampler inside next still applies.
func Core(next zapcore.Core) zapcore.Core {
	return &core{next: next}
}

// core folds the entries that carry a context and forwards every other entry. A logger
// built with Bind carries its context through With, so the core holds it here.
type core struct {
	next zapcore.Core
	held []zapcore.Field // the fields from With, with no context
	ctx  context.Context // the context of a bound logger, if any
}

// Enabled defers to next, so the level rules stay with the app.
func (c *core) Enabled(level zapcore.Level) bool { return c.next.Enabled(level) }

// With keeps the fields and the context of a bound logger. The context field never
// reaches the wrapped core.
func (c *core) With(fields []zapcore.Field) zapcore.Core {
	ctx, rest := contextOf(fields)
	held := make([]zapcore.Field, 0, len(c.held)+len(rest))
	held = append(held, c.held...)
	held = append(held, rest...)
	if ctx == nil {
		ctx = c.ctx
	}
	return &core{next: c.next.With(rest), held: held, ctx: ctx}
}

// Sync flushes the wrapped core.
func (c *core) Sync() error { return c.next.Sync() }

// Check declares this core on the wrapper type, never through an embedded core, so the
// wrapper joins every checked entry that its own level accepts.
func (c *core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !c.Enabled(ent.Level) {
		return ce
	}
	return ce.AddCore(ent, c)
}

// Write folds one entry into the event of its context field, and it removes that field.
// With no event, it checks next again and writes, so an inner core still decides.
func (c *core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	ctx, rest := contextOf(fields)
	if ctx == nil {
		ctx = c.ctx
	}
	if ctx != nil && wlog.HasEvent(ctx) {
		all := make([]zapcore.Field, 0, len(c.held)+len(rest))
		all = append(all, c.held...)
		all = append(all, rest...)
		wlog.AppendLog(ctx, lineOf(ent, all))
		return nil
	}
	if ce := c.next.Check(ent, nil); ce != nil {
		ce.Write(rest...)
	}
	return nil
}

// contextOf returns the context of one entry and its other fields.
func contextOf(fields []zapcore.Field) (context.Context, []zapcore.Field) {
	for i, field := range fields {
		ctx, ok := field.Interface.(context.Context)
		if !ok {
			continue
		}
		rest := make([]zapcore.Field, 0, len(fields)-1)
		rest = append(rest, fields[:i]...)
		rest = append(rest, fields[i+1:]...)
		return ctx, rest
	}
	return nil, fields
}

// lineOf converts one entry and its fields into the shape core folds into logs[].
func lineOf(ent zapcore.Entry, fields []zapcore.Field) wlog.LogLine {
	line := wlog.LogLine{Level: strings.ToLower(ent.Level.String()), Msg: ent.Message}
	if !ent.Time.IsZero() {
		line.Time = ent.Time.Format(time.RFC3339Nano)
	}
	if len(fields) > 0 {
		line.Attrs = attrsOf(fields)
	}
	return line
}

// attrsOf renders fields through the map encoder of zap, so a nested object stays an
// object and a zap.Error becomes its message.
func attrsOf(fields []zapcore.Field) map[string]any {
	encoder := zapcore.NewMapObjectEncoder()
	for _, field := range fields {
		field.AddTo(encoder)
	}
	return encoder.Fields
}

// Plugin returns a plugin that stores a logger bound to the context of every new unit of
// work, so the app's own lookup, such as ctxzap.FromContext, returns it for the rest of
// the unit. The store function keeps the app's convention. A nil store leaves the context
// unchanged.
func Plugin(base *zap.Logger, store func(context.Context, *zap.Logger) context.Context) wlog.Plugin {
	return &plugin{base: base, store: store}
}

// plugin binds a logger into the event context.
type plugin struct {
	base  *zap.Logger
	store func(context.Context, *zap.Logger) context.Context
}

// Name names the plugin.
func (*plugin) Name() string { return "wlogzap" }

// OnStart stores the bound logger in the context of one new unit of work.
func (p *plugin) OnStart(ctx context.Context, _ string) context.Context {
	if p.base == nil || p.store == nil {
		return ctx
	}
	return p.store(ctx, Bind(ctx, p.base))
}
