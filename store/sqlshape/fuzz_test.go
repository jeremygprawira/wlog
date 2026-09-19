// This file holds the fuzz target of the shaper. It proves that no input panics, and that
// every shape stays bounded and valid UTF-8. The literal forms of the pitfall table are
// the seeds, so the target starts on the cases that matter most.
package sqlshape_test

import (
	"testing"
	"unicode/utf8"

	"github.com/jeremygprawira/wlog/store/sqlshape"
)

// FuzzSQLShape proves that the shaper never panics, and that its output stays valid UTF-8
// and within the cap plus the mark, for any input and any dialect.
func FuzzSQLShape(f *testing.F) {
	seeds := []string{
		"SELECT * FROM users WHERE id = 42",
		"SELECT $$SECRET$$",
		"SELECT $tag$SECRET",
		"SELECT 'it''s SECRET'",
		"SELECT 'a\\'SECRET'",
		`SELECT "SECRET"`,
		"SELECT `SECRET`",
		"SELECT [SECRET]",
		"SELECT E'\\'SECRET'",
		"SELECT N'SECRET'",
		"SELECT 1 /* a /* b */ SECRET */",
		"SELECT 1 -- SECRET\nFROM t",
		"SELECT 1 # SECRET\nFROM t",
		"SELECT $1, $22 FROM t",
		"SELECT 12345678901234567890 AS n",
	}
	for _, seed := range seeds {
		f.Add(seed, uint8(0))
	}
	f.Fuzz(func(t *testing.T, query string, d uint8) {
		got := sqlshape.Shape(query, sqlshape.Dialect(d%5), 256)
		if len(got) > 256+len(sqlshape.Ellipsis) {
			t.Fatalf("Shape(%q, %d) = %q, which is over the cap", query, d%5, got)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("Shape(%q, %d) = %q, which is not valid UTF-8", query, d%5, got)
		}
	})
}
