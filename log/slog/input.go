// This file holds the input direction: the slog handler that folds a record into the
// open event.
package wlogslog

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
)

// Handler wraps next so that a record logged with a context carrying a wlog event is
// folded into that event's logs[] instead of reaching next. A record without such a
// context passes through untouched, so slog.SetDefault(slog.New(wlogslog.Handler(...)))
// stays safe even before every call site runs inside an event.
func Handler(next slog.Handler) slog.Handler {
	return &handler{next: next}
}

// handler is the slog.Handler returned by Handler. entries holds the ordered WithGroup
// names and WithAttrs attrs, so a folded line keeps the same nesting the wrapped handler
// would have produced.
type handler struct {
	next    slog.Handler
	entries []any // a string for a group name, and an slog.Attr for an attr
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

// WithAttrs keeps the attrs, at the depth the groups give them, and passes them down to
// next as usual.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	next := h.next.WithAttrs(attrs)
	entries := make([]any, 0, len(h.entries)+len(attrs))
	entries = append(entries, h.entries...)
	for _, attr := range attrs {
		entries = append(entries, attr)
	}
	return &handler{next: next, entries: entries}
}

// WithGroup keeps the group name for folding and passes it down to next as usual. An
// empty name returns the same handler, as slog asks.
func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	entries := make([]any, 0, len(h.entries)+1)
	entries = append(entries, h.entries...)
	entries = append(entries, name)
	return &handler{next: h.next.WithGroup(name), entries: entries}
}

// logLine converts a slog record, plus any WithAttrs/WithGroup state, into the shape
// core folds into logs[]. The level is lower-cased to match core's Level strings.
func (h *handler) logLine(r slog.Record) wlog.LogLine {
	line := wlog.LogLine{Level: strings.ToLower(r.Level.String()), Msg: r.Message}
	if !r.Time.IsZero() {
		// slogtest requires the time of a record, and it requires no time for a
		// zero record time, so the line carries one only when the record has one.
		line.Time = r.Time.Format(time.RFC3339Nano)
	}
	if attrs := orderedAttrs(h.entries, r); len(attrs) > 0 {
		line.Attrs = attrs
	}
	return line
}

// orderedAttrs builds the attr tree of one record: the WithAttrs attrs at the depth the
// groups give them, and the attrs of the record innermost.
func orderedAttrs(entries []any, r slog.Record) map[string]any {
	root := map[string]any{}
	current := root
	for _, entry := range entries {
		switch item := entry.(type) {
		case string:
			group := map[string]any{}
			current[item] = group
			current = group
		case slog.Attr:
			addAttr(current, item)
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		addAttr(current, a)
		return true
	})
	prune(root)
	return root
}

// addAttr renders one attr into a map. A zero attr is dropped, an empty group is dropped,
// and a group with an empty key is inlined, which is what slog's own handlers do.
func addAttr(into map[string]any, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	value := a.Value.Resolve()
	if value.Kind() != slog.KindGroup {
		if a.Key == "" {
			return
		}
		into[a.Key] = value.Any()
		return
	}
	group := map[string]any{}
	for _, item := range value.Group() {
		addAttr(group, item)
	}
	if len(group) == 0 {
		return
	}
	if a.Key == "" {
		for key, item := range group {
			into[key] = item
		}
		return
	}
	into[a.Key] = group
}

// prune removes the groups that hold no attr.
func prune(m map[string]any) {
	for key, value := range m {
		group, ok := value.(map[string]any)
		if !ok {
			continue
		}
		prune(group)
		if len(group) == 0 {
			delete(m, key)
		}
	}
}
