// This file builds the summary line: the one sentence a reader sees first. It runs in
// the finalize stage, on the redacted event, so a masked value can never reappear in
// the summary of the event that hid it.
package wlog

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// maxSummary caps the summary, so a wide event cannot push a reader's first line off
// the screen.
const maxSummary = 240

// maxPart caps one part of a summary. A longer value is left out rather than cut,
// because half a route or half an id misleads.
const maxPart = 64

// Event is the read-only view of one event that a hook, a keeper, or a summary builder
// reads. A field is reached by its dotted path, such as "http.status".
type Event interface {
	Get(path string) (any, bool)
	Kind() string
	Level() Level
}

// WithSummary replaces the builder of the summary line. The function receives the
// redacted event, and its result becomes the summary. A panic inside it falls back to
// the default builder, so a bad builder never costs the event.
func WithSummary(fn func(Event) string) Option {
	return func(l *Logger) { l.summary = fn }
}

// eventView reads one settled event. It is a snapshot, because the stages that call it
// must not change what the drains receive.
type eventView struct {
	fields map[string]any
	kind   string
	level  Level
}

// Get returns the value at a dotted path, such as "http.status".
func (v eventView) Get(path string) (any, bool) {
	var current any = v.fields
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// Kind returns the kind of the event.
func (v eventView) Kind() string { return v.kind }

// Level returns the level of the event.
func (v eventView) Level() Level { return v.level }

// summarizer builds one summary. It returns the default text, and reports the hook
// panic when a caller's builder fails.
func (l *Logger) summarizer(ctx context.Context, event map[string]any, masked string) string {
	fallback := defaultSummary(event, masked)
	if l.summary == nil {
		return fallback
	}
	if custom := l.callSummary(ctx, viewOf(event)); custom != "" {
		return custom
	}
	return fallback
}

// callSummary runs the caller's builder under recover, so a panicking builder reports
// itself and leaves the default summary in place.
func (l *Logger) callSummary(ctx context.Context, view eventView) (out string) {
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeHookPanic, "summary", fmt.Errorf("panic: %v", r))
			out = ""
		}
	}()
	return l.summary(view)
}

// levelFrom reads the level of an event, which its text form holds.
func levelFrom(event map[string]any) Level {
	text, _ := event["level"].(string)
	return Level(text)
}

// defaultSummary builds the summary of one kind, then the error suffix, then up to two
// user keys that end in _id. SPEC-core-v2 fixes each template.
func defaultSummary(event map[string]any, masked string) string {
	kind, _ := event["kind"].(string)
	duration := durationText(numberAt(event, "duration_ms"))
	part := func(path string) string { return summaryPart(event, path, masked) }

	var head string
	switch kind {
	case "request":
		route := part("http.route")
		if route == "" {
			route = "unmatched"
		}
		head = join(part("http.method"), route, part("http.status"), "in "+duration)
	case "rpc":
		head = join(part("rpc.system"), part("rpc.service")+"/"+part("rpc.method"),
			part("rpc.status_code"), "in "+duration)
	case "message":
		head = join(part("messaging.operation"), part("messaging.destination"),
			part("outcome"), "in "+duration)
		if n := int(numberAt(event, "messaging.delivery_count")); n > 1 {
			head = join(head, fmt.Sprintf("delivery %d", n))
		}
	case "job":
		head = join("job", part("job.name"), part("outcome"), "in "+duration)
		if n := int(numberAt(event, "job.attempt")); n > 1 {
			head = join(head, fmt.Sprintf("attempt %d", n))
		}
	case "command":
		head = join(part("cli.path"), "exit "+part("cli.exit_code"), "in "+duration)
	case "function":
		head = join("function", part("faas.name"), part("faas.trigger"), part("outcome"),
			"in "+duration)
		if cold, ok := event["faas"].(map[string]any); ok && cold["cold_start"] == true {
			head = join(head, "cold start")
		}
	case "log":
		head = part("message")
	default:
		head = join(part("operation"), part("outcome"), "in "+duration)
	}

	// The error suffix attaches to the sentence, and the id keys follow it with a
	// space, which is how a reader expects to see ": CODE".
	return clip(join(head+errorSuffix(event, masked), idSuffix(event, masked)))
}

// errorSuffix renders ": CODE message (fix: ...)" for an event with an error, leaving
// out every empty part.
func errorSuffix(event map[string]any, masked string) string {
	detail, ok := event["error"].(map[string]any)
	if !ok {
		return ""
	}
	code := usableText(detail["code"], masked)
	message := usableText(detail["message"], masked)
	fix := usableText(detail["fix"], masked)
	if code == "" && message == "" && fix == "" {
		return ""
	}
	text := join(code, message)
	if text == "" {
		return ""
	}
	if fix != "" {
		text = fmt.Sprintf("%s (fix: %s)", text, fix)
	}
	return ": " + text
}

// usableText returns a text value that a summary may hold, and an empty string for a
// masked, over-long, or absent one.
func usableText(value any, masked string) string {
	text, ok := value.(string)
	if !ok || isMasked(text, masked) || len(text) > maxPart {
		return ""
	}
	return text
}

// idSuffix renders " (key=value)" for up to two user keys that end in _id, sorted by
// name, so two events of one flow read alike.
func idSuffix(event map[string]any, masked string) string {
	keys := make([]string, 0, 2)
	for key := range event {
		if strings.HasSuffix(key, "_id") && !isReservedKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > 2 {
		keys = keys[:2]
	}
	parts := make([]string, 0, 2)
	for _, key := range keys {
		value := summaryPart(event, key, masked)
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("(%s=%s)", key, value))
	}
	return strings.Join(parts, " ")
}

// isReservedKey reports whether a key belongs to the reserved table rather than to the
// caller, so an id in the trace group is not repeated in the summary.
// IsReservedKey reports whether key is a reserved key of the event shape. A preset and
// a drain call it to tell a user key from a canonical one.
func IsReservedKey(key string) bool { return isReservedKey(key) }

// isReservedKey reports whether key is a reserved key of the event shape.
func isReservedKey(key string) bool {
	_, reserved := reservedRank[key]
	if reserved {
		return true
	}
	_, tail := reservedTailRank[key]
	return tail || key == reservedTail[0]
}

// summaryPart returns one value as summary text, and an empty string when the value is
// absent, masked, or longer than a part may be.
func summaryPart(event map[string]any, path, masked string) string {
	value, ok := pathValue(event, path)
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		if isMasked(v, masked) || len(v) > maxPart {
			return ""
		}
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	}
	return ""
}

// isMasked reports whether text holds the redactor's replacement, so a value the
// redactor hid never reappears inside the summary.
func isMasked(text, masked string) bool {
	if text == "" {
		return false
	}
	if masked != "" && strings.Contains(text, masked) {
		return true
	}
	return strings.Contains(text, "[REDACTED")
}

// pathValue reads a dotted path from an event map.
func pathValue(event map[string]any, path string) (any, bool) {
	var current any = event
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// numberAt reads a number at a dotted path, and returns 0 when the path is absent.
func numberAt(event map[string]any, path string) float64 {
	value, ok := pathValue(event, path)
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case time.Duration:
		return float64(v.Microseconds()) / 1000
	}
	return 0
}

// durationText renders a duration the way a reader speaks it: 0.84ms, 12.8ms, 1.24s.
func durationText(ms float64) string {
	switch {
	case ms <= 0:
		return ""
	case ms < 1:
		return fmt.Sprintf("%.2fms", ms)
	case ms < 1000:
		return fmt.Sprintf("%.1fms", ms)
	default:
		return fmt.Sprintf("%.2fs", ms/1000)
	}
}

// join renders the non-empty parts, one space apart, so a missing part costs no
// double space.
func join(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

// clip cuts a summary at the cap, so the first line of a wide event stays short.
func clip(text string) string {
	runes := []rune(text)
	if len(runes) <= maxSummary {
		return text
	}
	return string(runes[:maxSummary])
}
