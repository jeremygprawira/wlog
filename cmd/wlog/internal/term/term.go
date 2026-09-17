// Package term holds the little the CLI needs to know about a terminal: whether to color its
// output, how wide the output should be, and how to wrap a line to that width. It reads two
// environment variables, NO_COLOR and COLUMNS, because that is what a caller can set from a
// script, a CI job, or a pipe.
package term

import (
	"os"
	"strconv"
	"strings"
)

// defaultWidth is the wrap width when COLUMNS says nothing: the classic terminal width, and a
// width a table still reads at.
const defaultWidth = 100

// minWidth is the narrowest a wrapped report may become, so a tiny COLUMNS still reads.
const minWidth = 20

// ColorEnabled reports whether the output may carry ANSI color codes. NO_COLOR, whatever its
// value, turns them off; so does a writer that is not a terminal.
func ColorEnabled() bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	return IsTerminal(os.Stdout)
}

// IsTerminal reports whether f is a character device, which is how a pipe is told from a
// terminal without a dependency.
func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Width is the column count the report should wrap at: COLUMNS when it holds a number, and the
// default otherwise. A width below the minimum is raised, so a stray value never shreds the
// report into unreadable columns.
func Width() int {
	raw := strings.TrimSpace(os.Getenv("COLUMNS"))
	if raw == "" {
		return defaultWidth
	}
	width, err := strconv.Atoi(raw)
	if err != nil || width <= 0 {
		return defaultWidth
	}
	if width < minWidth {
		return minWidth
	}
	return width
}

// Wrap returns text with every line broken at width columns, at the last space before the limit.
// The break continues on the next line with no indent, because a report line reads as one
// sentence.
func Wrap(text string, width int) string {
	if width < minWidth {
		width = minWidth
	}
	var out strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(wrapLine(line, width))
	}
	return out.String()
}

// wrapLine breaks one line at width columns.
func wrapLine(line string, width int) string {
	if len(line) <= width {
		return line
	}
	var out strings.Builder
	rest := line
	for len(rest) > width {
		cut := strings.LastIndex(rest[:width+1], " ")
		if cut <= 0 {
			// A single long token, such as a long path or a URL, is cut where the width ends.
			cut = width
		}
		tail := strings.TrimLeft(rest[cut:], " ")
		if strings.HasPrefix(strings.ToLower(tail), "http") {
			// A URL stays whole: a split link is hard to copy and hard to read.
			break
		}
		out.WriteString(strings.TrimRight(rest[:cut], " "))
		out.WriteByte('\n')
		rest = tail
	}
	out.WriteString(rest)
	return out.String()
}
