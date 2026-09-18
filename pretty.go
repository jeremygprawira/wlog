// This file renders one event for a terminal: the summary line, the error block, one
// tree line per group, one line per call and per folded log record, and the user keys
// last. Core renders the whole event into one buffer, so one event is one write and two
// events can never interleave.
package wlog

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Format picks the stdout rendering. FormatAuto (the zero value, and the default) is
// pretty in local/dev (per WithService's env) and JSON everywhere else.
type Format int

const (
	FormatAuto Format = iota
	FormatJSON
	FormatPretty
)

// WithFormat overrides FormatAuto's environment-based choice.
func WithFormat(f Format) Option {
	return func(l *Logger) { l.format = f }
}

// resolvedFormat returns the format of the stdout sink. FormatAuto picks pretty for a
// terminal, or for env local, dev, or development, and JSON everywhere else.
func (l *Logger) resolvedFormat() Format {
	if l.format != FormatAuto {
		return l.format
	}
	switch l.service.env {
	case "local", "dev", "development":
		return FormatPretty
	}
	if isTerminal(os.Stdout) {
		return FormatPretty
	}
	return FormatJSON
}

var levelColor = map[Level]string{
	LevelDebug: "\033[90m",
	LevelInfo:  "\033[36m",
	LevelWarn:  "\033[33m",
	LevelError: "\033[31m",
}

const colorReset = "\033[0m"

// colorEnabled follows the NO_COLOR convention (no-color.org) and the terminal rule:
// colors are on only for a terminal, and NO_COLOR turns them off.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal(os.Stdout)
}

// isTerminal reports whether f is a character device, which is how a terminal shows up
// without a dependency outside the standard library.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// prettyGroups lists the object groups of an event in the order a reader reads them.
// Each one prints as a single tree line of key=value pairs.
var prettyGroups = []string{
	"service", "http", "rpc", "messaging", "job", "cli", "faas",
	"user", "geo", "client", "host", "deploy", "llm", "trace", "call_stats",
}

// prettyArrays lists the array groups of an event. Each one prints one line per element.
var prettyArrays = []string{"calls", "logs", "errors", "audit", "feature_flags"}

// maxPrettyValue caps one value on a pretty line, so a long field cannot push the rest of
// the line off the screen.
const maxPrettyValue = 80

// prettyLine is one tree line of a rendered event. A line with no label holds the user
// keys, and it prints without the name column.
type prettyLine struct {
	label string
	text  string
}

// renderPretty renders one event in the layout of SPEC-core-v2. color adds an escape
// around the level. The whole event lands in one buffer, so a caller writes it once.
func renderPretty(event map[string]any, color bool) []byte {
	var buf bytes.Buffer
	buf.WriteString(prettyHead(event, color))
	buf.WriteByte('\n')
	for _, line := range prettyError(event) {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	lines := prettyBody(event)
	width := 0
	for _, line := range lines {
		if len(line.label)+2 > width {
			width = len(line.label) + 2
		}
	}
	for i, line := range lines {
		glyph := "├─"
		if i == len(lines)-1 {
			glyph = "└─"
		}
		buf.WriteString("  " + glyph + " ")
		if line.label != "" {
			buf.WriteString(line.label)
			buf.WriteString(strings.Repeat(" ", width-len(line.label)))
		}
		buf.WriteString(line.text)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// prettyHead renders the first line: the colored level and the one sentence of the event,
// without the fix, because the error block prints the fix on its own line.
func prettyHead(event map[string]any, color bool) string {
	level, _ := event["level"].(string)
	label := strings.ToUpper(level)
	if color {
		if code, ok := levelColor[Level(level)]; ok {
			label = code + label + colorReset
		}
	}
	return label + " " + prettySummary(event)
}

// prettySummary returns the sentence of an event without its "(fix: ...)" tail, and falls
// back to the operation or the message when the event carries no sentence.
func prettySummary(event map[string]any) string {
	summary, _ := event["summary"].(string)
	if fix, ok := errorText(event, "fix"); ok && fix != "" {
		summary = strings.Replace(summary, " (fix: "+fix+")", "", 1)
	}
	if summary != "" {
		return summary
	}
	if operation, _ := event["operation"].(string); operation != "" {
		return operation
	}
	message, _ := event["message"].(string)
	return message
}

// prettyError renders the error block: why, fix, link, and the caller of the wlog.Error
// call, each on its own line. It returns nothing for an event with no error.
func prettyError(event map[string]any) []string {
	if _, ok := event["error"].(map[string]any); !ok {
		return nil
	}
	lines := make([]string, 0, 4)
	for _, field := range []struct{ label, key string }{
		{"Why:", "why"}, {"Fix:", "fix"}, {"More:", "link"},
	} {
		text, _ := errorText(event, field.key)
		if text == "" {
			continue
		}
		lines = append(lines, "  "+field.label+strings.Repeat(" ", 6-len(field.label))+clipPretty(text))
	}
	if caller, _ := errorText(event, "caller"); caller != "" {
		lines = append(lines, "  at "+clipPretty(caller))
	}
	return lines
}

// errorText reads one text field of the error object of an event.
func errorText(event map[string]any, key string) (string, bool) {
	errInfo, ok := event["error"].(map[string]any)
	if !ok {
		return "", false
	}
	text, ok := errInfo[key].(string)
	return text, ok
}

// prettyBody renders the tree lines after the error block: one line per group, one line
// per call and per log record, and the user keys last.
func prettyBody(event map[string]any) []prettyLine {
	lines := make([]prettyLine, 0, len(event))
	for _, name := range prettyGroups {
		object, ok := event[name].(map[string]any)
		if !ok {
			continue
		}
		if text := prettyPairs(name, object); text != "" {
			lines = append(lines, prettyLine{label: name, text: text})
		}
	}
	for _, name := range prettyArrays {
		array, ok := event[name].([]any)
		if !ok {
			continue
		}
		for _, element := range array {
			if text := prettyElement(name, element); text != "" {
				lines = append(lines, prettyLine{label: name, text: text})
			}
		}
	}
	if text := prettyUserKeys(event); text != "" {
		lines = append(lines, prettyLine{text: text})
	}
	return lines
}

// prettyUserKeys renders every user key as one line of key=value pairs, sorted by name.
func prettyUserKeys(event map[string]any) string {
	keys := make([]string, 0, len(event))
	for key := range event {
		if !isReservedKey(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+prettyValue(event[key]))
	}
	return strings.Join(parts, " ")
}

// prettyPairs renders one object as key=value pairs, in the order the group table fixes
// and then sorted by name, so two events with the same fields read the same way.
func prettyPairs(name string, object map[string]any) string {
	parts := make([]string, 0, len(object))
	for _, key := range orderedNested(nestedKeyOrder[name], object) {
		if text, ok := prettyField(object[key]); ok {
			parts = append(parts, key+"="+text)
		}
	}
	return strings.Join(parts, " ")
}

// prettyElement renders one element of an array group.
//
// A call reads as its operation, target, status, and duration, a log record reads as its
// level, message, and attributes, and any other element reads as key=value pairs.
func prettyElement(name string, element any) string {
	object, ok := element.(map[string]any)
	if !ok {
		return prettyValue(element)
	}
	switch name {
	case "calls":
		return prettyCall(object)
	case "logs":
		return prettyLog(object)
	default:
		return prettyPairs(name, object)
	}
}

// prettyCall renders one call: what it did, where, its status, and how long it took.
func prettyCall(call map[string]any) string {
	parts := make([]string, 0, 5)
	for _, key := range []string{"operation", "target", "status"} {
		if text, ok := prettyField(call[key]); ok {
			parts = append(parts, text)
		}
	}
	if took := durationText(numberAt(call, "duration_ms")); took != "" {
		parts = append(parts, took)
	}
	if code, _ := errorText(map[string]any{"error": call["error"]}, "code"); code != "" {
		parts = append(parts, code)
	}
	return strings.Join(parts, " ")
}

// prettyLog renders one folded log record: its level, its message, and its attributes.
func prettyLog(line map[string]any) string {
	parts := make([]string, 0, 3)
	for _, key := range []string{"level", "msg"} {
		if text, ok := prettyField(line[key]); ok {
			parts = append(parts, text)
		}
	}
	if attrs, ok := line["attrs"].(map[string]any); ok {
		if text := prettyPairs("", attrs); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

// prettyField returns one value as pretty text, and reports whether it carries anything.
// An absent value and an empty string carry nothing.
func prettyField(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	text := prettyValue(value)
	return text, text != ""
}

// prettyValue renders one value as text. A nested object or array becomes a marker,
// because a tree line shows the fields of a group and not the shape of its values.
func prettyValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return clipPretty(v)
	case map[string]any:
		return "{...}"
	case []any:
		return "[...]"
	}
	return clipPretty(fmt.Sprint(value))
}

// clipPretty cuts a value past maxPrettyValue characters and marks the cut.
func clipPretty(text string) string {
	runes := []rune(text)
	if len(runes) <= maxPrettyValue {
		return text
	}
	return string(runes[:maxPrettyValue]) + "…"
}
