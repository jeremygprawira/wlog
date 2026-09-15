package wlogslog

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Handler wraps next so that a record logged with a context carrying a wlog event is
// folded into that event's logs[] instead of reaching next. A record without such a
// context passes through untouched, so slog.SetDefault(slog.New(wlogslog.Handler(...)))
// stays safe even before every call site runs inside an event.
func Handler(next slog.Handler) slog.Handler {
	return &handler{next: next}
}

// handler is the slog.Handler returned by Handler. attrs and groups collect the
// WithAttrs/WithGroup calls made on the way down, so a folded line keeps the same
// nesting the wrapped handler would have produced.
type handler struct {
	next   slog.Handler
	attrs  []slog.Attr
	groups []string
}

// Enabled reports true whenever ctx carries a wlog event, so slog never filters out a
// record that belongs in logs[]; otherwise it defers to next.
func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	if wlog.HasEvent(ctx) {
		return true
	}
	return h.next.Enabled(ctx, level)
}

// Handle folds r into the event in ctx, or forwards r to next when there is none.
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if !wlog.HasEvent(ctx) {
		return h.next.Handle(ctx, r)
	}
	wlog.AppendLog(ctx, h.logLine(r))
	return nil
}

// WithAttrs keeps the attrs for folding and passes them down to next as usual.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := h.next.WithAttrs(attrs)
	combined := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	combined = append(combined, h.attrs...)
	combined = append(combined, attrs...)
	return &handler{next: next, attrs: combined, groups: h.groups}
}

// WithGroup keeps the group name for folding and passes it down to next as usual.
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := make([]string, 0, len(h.groups)+1)
	groups = append(groups, h.groups...)
	groups = append(groups, name)
	return &handler{next: h.next.WithGroup(name), attrs: h.attrs, groups: groups}
}

// logLine converts a slog record, plus any WithAttrs/WithGroup state, into the shape
// core folds into logs[]. The level is lower-cased to match core's Level strings.
func (h *handler) logLine(r slog.Record) wlog.LogLine {
	attrs := make([]slog.Attr, 0, len(h.attrs)+int(r.NumAttrs()))
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	line := wlog.LogLine{Level: strings.ToLower(r.Level.String()), Msg: r.Message}
	if len(attrs) > 0 {
		line.Attrs = attrsToMap(attrs, h.groups)
	}
	return line
}

// attrsToMap renders attrs to a plain map, recursing into slog groups. groups wraps the
// whole result, matching slog's own WithGroup nesting.
func attrsToMap(attrs []slog.Attr, groups []string) map[string]any {
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		value := a.Value.Resolve()
		if value.Kind() == slog.KindGroup {
			group := attrsToMap(value.Group(), nil)
			if len(group) == 0 {
				continue
			}
			m[a.Key] = group
			continue
		}
		m[a.Key] = value.Any()
	}
	for i := len(groups) - 1; i >= 0; i-- {
		m = map[string]any{groups[i]: m}
	}
	return m
}
