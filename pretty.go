package wlog

import (
	"fmt"
	"io"
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

func (l *Logger) resolvedFormat() Format {
	if l.format != FormatAuto {
		return l.format
	}
	if l.service.env == "local" || l.service.env == "dev" {
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

// colorEnabled follows the NO_COLOR convention (no-color.org): colors are on unless
// NO_COLOR is set to a non-empty value.
func colorEnabled() bool {
	return os.Getenv("NO_COLOR") == ""
}

// prettyFields are shown in the header line rather than as a plain "key: value" row.
var prettyFields = map[string]bool{
	"timestamp": true, "level": true, "operation": true, "duration_ms": true,
	"outcome": true, "message": true, "kind": true, "error": true,
}

// writePretty writes one human-readable, tree-style rendering of event to w: a header
// (level, operation or message, duration, outcome), one indented line per remaining
// field, and — if the event carries an error — a why/fix/link block, similar to
// evlog's dev console.
func writePretty(w io.Writer, event map[string]any, color bool) {
	level, _ := event["level"].(string)
	label := strings.ToUpper(level)
	if color {
		if c, ok := levelColor[Level(level)]; ok {
			label = c + label + colorReset
		}
	}

	name, _ := event["operation"].(string)
	if name == "" {
		name, _ = event["message"].(string)
	}

	header := fmt.Sprintf("%s %s", label, name)
	if d, ok := event["duration_ms"]; ok {
		header += fmt.Sprintf(" (%vms)", d)
	}
	if outcome, ok := event["outcome"].(string); ok && outcome != "success" {
		header += " [" + outcome + "]"
	}
	fmt.Fprintln(w, header)

	keys := make([]string, 0, len(event))
	for k := range event {
		if !prettyFields[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %s: %v\n", k, event[k])
	}

	if errInfo, ok := event["error"].(map[string]any); ok {
		fmt.Fprintf(w, "  error: %v\n", errInfo["message"])
		if why, _ := errInfo["why"].(string); why != "" {
			fmt.Fprintf(w, "    Why: %s\n", why)
		}
		if fix, _ := errInfo["fix"].(string); fix != "" {
			fmt.Fprintf(w, "    Fix: %s\n", fix)
		}
		if link, _ := errInfo["link"].(string); link != "" {
			fmt.Fprintf(w, "    More: %s\n", link)
		}
	}
}
