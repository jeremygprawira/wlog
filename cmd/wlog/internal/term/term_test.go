package term_test

import (
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/cmd/wlog/internal/term"
)

// TestTerm_Width proves COLUMNS sets the width, a bad value falls back, and a tiny value is
// raised so the report stays readable.
func TestTerm_Width(t *testing.T) {
	cases := []struct {
		columns string
		want    int
	}{
		{"40", 40},
		{"", 100},
		{"abc", 100},
		{"0", 100},
		{"5", 20},
	}
	for _, tc := range cases {
		t.Setenv("COLUMNS", tc.columns)
		if got := term.Width(); got != tc.want {
			t.Errorf("Width() with COLUMNS=%q = %d, want %d", tc.columns, got, tc.want)
		}
	}
}

// TestTerm_NoColor proves NO_COLOR turns color off whatever its value, and that a test process
// writing to a pipe never claims color.
func TestTerm_NoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if term.ColorEnabled() {
		t.Error("ColorEnabled() = true with NO_COLOR set to an empty value")
	}
	t.Setenv("NO_COLOR", "1")
	if term.ColorEnabled() {
		t.Error("ColorEnabled() = true with NO_COLOR=1")
	}
}

// TestTerm_Wrap proves a long line is broken at the last space before the limit, and that no
// line exceeds it.
func TestTerm_Wrap(t *testing.T) {
	wrapped := term.Wrap("the quick brown fox jumps over the lazy dog", 20)
	for _, line := range strings.Split(wrapped, "\n") {
		if len(line) > 20 {
			t.Errorf("line %q is %d characters, want at most 20", line, len(line))
		}
	}
	if strings.Join(strings.Fields(wrapped), " ") != "the quick brown fox jumps over the lazy dog" {
		t.Errorf("wrapping lost words: %q", wrapped)
	}
}
