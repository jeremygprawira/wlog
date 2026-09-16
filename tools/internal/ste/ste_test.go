// Package ste_test proves the linter against the Python linter it was ported
// from.
//
// Each fixture under tools/testdata/ste holds one class of mistake. The table
// holds the count that the Python linter reports for every category, so a change
// in the Go port that the Python linter would not make fails this test.
package ste_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/tools/internal/ste"
)

// fixture reads one fixture file.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ste", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSte_MatchesPythonCounts proves the port reproduces the Python result on
// five fixture files, category by category.
func TestSte_MatchesPythonCounts(t *testing.T) {
	cases := []struct {
		file   string
		counts map[string]int
	}{
		{"slop.md", map[string]int{
			"banned_modal": 1, "contraction": 1, "ing_clause": 1, "latin_abbrev": 1,
			"perfect_tense": 1, "semicolon": 1, "sentence_over_limit": 1,
			"slop_word": 2, "synonym_rotation": 2, "trailing_condition": 1,
		}},
		{"clean.md", map[string]int{}},
		{"dashes.md", map[string]int{"em_dash": 3}},
		{"table.md", map[string]int{}},
		{"prose.md", map[string]int{
			"em_dash": 1, "ing_clause": 1, "latin_abbrev": 1, "semicolon": 1,
			"sentence_over_limit": 1, "trailing_condition": 2,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			report := ste.Lint(fixture(t, tc.file), ste.Descriptive)
			got := report.Counts()
			if len(got) != len(tc.counts) {
				t.Errorf("categories = %v, want %v", got, tc.counts)
			}
			for category, want := range tc.counts {
				if got[category] != want {
					t.Errorf("%s = %d, want %d (all: %v)", category, got[category], want, got)
				}
			}
		})
	}
}

// TestSte_ReportsLineAndText proves that every hit names the line to read and the
// text that caused it, so a report can point at the file.
func TestSte_ReportsLineAndText(t *testing.T) {
	report := ste.Lint(fixture(t, "slop.md"), ste.Descriptive)
	if len(report.Hits) == 0 {
		t.Fatal("no hits in the slop fixture")
	}
	for _, hit := range report.Hits {
		if hit.Line < 1 {
			t.Errorf("hit %+v has no line", hit)
		}
		if strings.TrimSpace(hit.Text) == "" {
			t.Errorf("hit %+v has no text", hit)
		}
	}
	first := report.Hits[0]
	if !strings.Contains(fixture(t, "slop.md"), first.Text[:min(10, len(first.Text))]) {
		t.Errorf("hit text %q is not in the file", first.Text)
	}
}

// TestSte_LimitFollowsRegister proves that procedural text holds a tighter limit
// than descriptive text.
func TestSte_LimitFollowsRegister(t *testing.T) {
	// Twenty-two words: over the procedural limit of twenty, under the
	// descriptive limit of twenty-five.
	text := "Open the file and read every line of it and then write the result into a new file on the same disk."

	if got := ste.Lint(text, ste.Procedural).Counts()["sentence_over_limit"]; got != 1 {
		t.Errorf("procedural hits = %d, want 1", got)
	}
	if got := ste.Lint(text, ste.Descriptive).Counts()["sentence_over_limit"]; got != 0 {
		t.Errorf("descriptive hits = %d, want 0", got)
	}
}

// TestSte_IgnoresCode proves that code, headings, and URLs never count, because
// they obey rules of their own.
func TestSte_IgnoresCode(t *testing.T) {
	text := "# Heading with a very long title that runs past any limit we set here\n\n" +
		"```go\n// A comment, which is a contraction: it's fine here.\nfunc f() {}\n```\n\n" +
		"See `should_not_count_it's` and https://example.com/should-not-count-either for more.\n"

	report := ste.Lint(text, ste.Descriptive)
	if report.Total() != 0 {
		t.Errorf("hits = %v, want none", report.Hits)
	}
}

// min returns the smaller of two numbers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
