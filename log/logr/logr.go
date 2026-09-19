// Package wloglogr folds logr, klog, and controller-runtime log calls into the open wlog
// event, and writes wlog events through a logr logger.
//
// Read top to bottom: Plugin binds a logr logger to the context of each unit of work, so
// logr.FromContext and klog.FromContext return it for the rest of the unit. Sink folds
// Info at V(0) as info and V(1) or higher as debug, folds Error as error, and forwards to
// the next sink when the event is gone. Drain writes each event as one logr record.
//
// This is the whole setup:
//
//	log := wlog.New(wlog.WithPlugins(wloglogr.Plugin()))
//
// klog's contextual logging reads the bound logger from the context while it is on.
package wloglogr

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-logr/logr"

	"github.com/jeremygprawira/wlog"
)

// Plugin returns a plugin that binds a logr logger to the context of every new unit of
// work. The bound logger folds into that event.
func Plugin() wlog.Plugin { return plugin{} }

// plugin binds a logger into the event context.
type plugin struct{}

// Name names the plugin.
func (plugin) Name() string { return "wloglogr" }

// OnStart wraps the logger already in the context, or the discard logger, and stores the
// wrapper back.
func (plugin) OnStart(ctx context.Context, _ string) context.Context {
	logger := logr.FromContextOrDiscard(ctx)
	return logr.NewContext(ctx, logr.New(Sink(ctx, logger.GetSink())))
}

// Sink returns a log sink that folds every Info and Error call into the open event of
// ctx. With no event, or after the event emitted, the call reaches next.
func Sink(ctx context.Context, next logr.LogSink) logr.LogSink {
	if next == nil {
		next = discardSink{}
	}
	return &sink{ctx: ctx, next: next}
}

// discardSink drops every call, for the case where the app had no logger in its context.
// logr's own discard logger holds a nil sink, so the bridge needs one of its own.
type discardSink struct{}

// Init does nothing.
func (discardSink) Init(logr.RuntimeInfo) {}

// Enabled reports false.
func (discardSink) Enabled(int) bool { return false }

// Info drops one message.
func (discardSink) Info(int, string, ...any) {}

// Error drops one error.
func (discardSink) Error(error, string, ...any) {}

// WithValues returns the same sink.
func (discardSink) WithValues(...any) logr.LogSink { return discardSink{} }

// WithName returns the same sink.
func (discardSink) WithName(string) logr.LogSink { return discardSink{} }

// sink folds the calls that carry an open event and forwards the rest.
type sink struct {
	ctx    context.Context
	next   logr.LogSink
	values []any
	names  []string
	attrs  []slog.Attr
	groups []string
}

// Init passes the runtime information to the wrapped sink.
func (s *sink) Init(info logr.RuntimeInfo) { s.next.Init(info) }

// Enabled reports true whenever the event is open, so logr never drops a line that
// belongs in logs[]; otherwise it defers to the wrapped sink.
func (s *sink) Enabled(level int) bool {
	return wlog.HasEvent(s.ctx) || s.next.Enabled(level)
}

// Info folds one message, and it forwards the call when the event is gone.
func (s *sink) Info(level int, msg string, kv ...any) {
	if !wlog.HasEvent(s.ctx) {
		s.next.Info(level, msg, kv...)
		return
	}
	wlog.AppendLog(s.ctx, s.line(levelName(level), msg, kv))
}

// Error folds one error, and it forwards the call when the event is gone. A nil error is
// allowed, and it adds no error attribute.
func (s *sink) Error(err error, msg string, kv ...any) {
	if !wlog.HasEvent(s.ctx) {
		s.next.Error(err, msg, kv...)
		return
	}
	line := s.line("error", msg, kv)
	if err != nil {
		if line.Attrs == nil {
			line.Attrs = map[string]any{}
		}
		line.Attrs["error"] = err.Error()
	}
	wlog.AppendLog(s.ctx, line)
}

// WithValues keeps the pairs for folding and passes them to the wrapped sink.
func (s *sink) WithValues(kv ...any) logr.LogSink {
	values := make([]any, 0, len(s.values)+len(kv))
	values = append(values, s.values...)
	values = append(values, kv...)
	return s.clone(func(n *sink) {
		n.values = values
		n.next = s.next.WithValues(kv...)
	})
}

// WithName appends one name, and it passes it to the wrapped sink.
func (s *sink) WithName(name string) logr.LogSink {
	names := make([]string, 0, len(s.names)+1)
	names = append(names, s.names...)
	names = append(names, name)
	return s.clone(func(n *sink) {
		n.names = names
		n.next = s.next.WithName(name)
	})
}

// WithCallDepth forwards the depth to the wrapped sink when it takes one.
func (s *sink) WithCallDepth(depth int) logr.LogSink {
	next := s.next
	if caller, ok := next.(logr.CallDepthLogSink); ok {
		next = caller.WithCallDepth(depth)
	}
	return s.clone(func(n *sink) { n.next = next })
}

// Handle folds one slog record, so logr.ToSlogHandler keeps the context. The record of a
// level below info folds as debug, and an error record folds as error.
func (s *sink) Handle(ctx context.Context, r slog.Record) error {
	target := s
	if wlog.HasEvent(ctx) {
		target = &sink{ctx: ctx, next: s.next, values: s.values, names: s.names, attrs: s.attrs, groups: s.groups}
	}
	kv := make([]any, 0, r.NumAttrs()*2)
	r.Attrs(func(a slog.Attr) bool {
		kv = append(kv, a.Key, a.Value.Resolve().Any())
		return true
	})
	switch {
	case r.Level >= slog.LevelError:
		target.Error(nil, r.Message, kv...)
	case r.Level >= slog.LevelInfo:
		target.Info(0, r.Message, kv...)
	default:
		target.Info(4, r.Message, kv...)
	}
	return nil
}

// WithAttrs keeps the attrs for folding and forwards them when the wrapped sink takes
// slog attrs.
func (s *sink) WithAttrs(attrs []slog.Attr) logr.SlogSink {
	combined := make([]slog.Attr, 0, len(s.attrs)+len(attrs))
	combined = append(combined, s.attrs...)
	combined = append(combined, attrs...)
	next := s.next
	if slogSink, ok := next.(logr.SlogSink); ok {
		next = slogSink.WithAttrs(attrs)
	}
	return s.cloneSlog(func(n *sink) {
		n.attrs = combined
		n.next = next
	})
}

// WithGroup keeps the group for folding and forwards it when the wrapped sink takes slog
// groups. An empty name returns the same sink.
func (s *sink) WithGroup(name string) logr.SlogSink {
	if name == "" {
		return s
	}
	groups := make([]string, 0, len(s.groups)+1)
	groups = append(groups, s.groups...)
	groups = append(groups, name)
	next := s.next
	if slogSink, ok := next.(logr.SlogSink); ok {
		next = slogSink.WithGroup(name)
	}
	return s.cloneSlog(func(n *sink) {
		n.groups = groups
		n.next = next
	})
}

// line builds one folded line from the sink state and one call.
func (s *sink) line(level, msg string, kv []any) wlog.LogLine {
	line := wlog.LogLine{Level: level, Msg: msg}
	attrs := map[string]any{}
	for i := 0; i+1 < len(s.values); i += 2 {
		if key, ok := s.values[i].(string); ok {
			attrs[key] = s.values[i+1]
		}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if key, ok := kv[i].(string); ok {
			attrs[key] = kv[i+1]
		}
	}
	for _, attr := range s.attrs {
		value := attr.Value.Resolve()
		if value.Kind() == slog.KindGroup || attr.Key == "" {
			continue
		}
		attrs[attr.Key] = value.Any()
	}
	if len(s.names) > 0 {
		attrs["logger"] = strings.Join(s.names, "/")
	}
	if len(attrs) > 0 {
		for i := len(s.groups) - 1; i >= 0; i-- {
			attrs = map[string]any{s.groups[i]: attrs}
		}
		line.Attrs = attrs
	}
	return line
}

// clone copies one sink with a change.
func (s *sink) clone(change func(*sink)) *sink {
	next := *s
	change(&next)
	return &next
}

// cloneSlog copies one sink and returns it as a slog sink.
func (s *sink) cloneSlog(change func(*sink)) logr.SlogSink { return s.clone(change) }

// levelName maps a logr verbosity to the level of a folded line. V(0) is info, and any
// higher verbosity is debug.
func levelName(level int) string {
	if level > 0 {
		return string(wlog.LevelDebug)
	}
	return string(wlog.LevelInfo)
}

// Drain returns a wlog.Drain that writes each event as one logr record. The event's
// operation becomes the message and its level becomes the verbosity or the error.
func Drain(logger logr.Logger) wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		kv := make([]any, 0, len(event))
		for key, value := range event {
			switch key {
			case "level", "operation", "message":
				continue
			}
			kv = append(kv, key, value)
		}
		switch event["level"] {
		case string(wlog.LevelError):
			logger.Error(nil, eventMessage(event), kv...)
		case string(wlog.LevelDebug):
			logger.V(1).Info(eventMessage(event), kv...)
		default:
			logger.Info(eventMessage(event), kv...)
		}
	})
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
