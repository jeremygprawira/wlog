// This file checks the statement shaper over the pitfall table of the spec: every literal
// form becomes a placeholder, an ambiguous construct stops the scan, a secret never
// survives, and the output stays bounded and valid UTF-8.
package sqlshape_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jeremygprawira/wlog/store/sqlshape"
)

// TestSQLShape_B1_Shapes proves that each statement becomes the shape of the spec table.
func TestSQLShape_B1_Shapes(t *testing.T) {
	cases := []struct {
		d        sqlshape.Dialect
		in, want string
	}{
		{sqlshape.Postgres, "SELECT * FROM users WHERE id = 42", "SELECT * FROM users WHERE id = ?"},
		{sqlshape.Postgres, "select a from t where b in (1, 2, 3) and c = 'x'", "select a from t where b in (?) and c = ?"},
		{sqlshape.Postgres, "SELECT $1, $22 FROM t WHERE x IN ($3,$4,$5)", "SELECT ?, ? FROM t WHERE x IN (?)"},
		{sqlshape.MySQL, "INSERT INTO t (a,b) VALUES (1,'x'),(2,'y')", "INSERT INTO t (a, b) VALUES (?)"},
		{sqlshape.Postgres, `SELECT "User"."Name" FROM "User"`, `SELECT "User"."Name" FROM "User"`},
		{sqlshape.MySQL, "SELECT `order` FROM `t` WHERE `a`=1.5e-3", "SELECT `order` FROM `t` WHERE `a`=?"},
		{sqlshape.SQLServer, "SELECT [col 1] FROM [dbo].[t] WHERE n = N'x'", "SELECT [col 1] FROM [dbo].[t] WHERE n = ?"},
		{sqlshape.Postgres, "SELECT '2020-01-01'::date, interval '1 day', DATE'2020'", "SELECT ?::date, interval ?, DATE ?"},
		{sqlshape.Postgres, "SELECT 0x1F, 1_000_000, .5, 5., X'FF', B'1010'", "SELECT ?, ?, ?, ?, ?, ?"},
		{sqlshape.MySQL, "SELECT * FROM 2fa_codes WHERE t1.c2 = 3", "SELECT * FROM 2fa_codes WHERE t1.c2 = ?"},
		{sqlshape.Postgres, "SELECT a$b$ FROM t", "SELECT a$b$ FROM t"},
		{sqlshape.Postgres, "SELECT data ? 'k', data @> '{}'", "SELECT data ? ?, data @> ?"},
		{sqlshape.Postgres, "SELECT 1 -- trailing\nFROM t /* a /* nested */ b */ WHERE x = :name", "SELECT ? FROM t WHERE x = :name"},
		{sqlshape.MySQL, "SELECT 1--1 FROM t /* a /* b */ WHERE c = ?1", "SELECT ?--? FROM t WHERE c = ?"},
		{sqlshape.Postgres, `SELECT 'C:\' , name FROM t`, "SELECT ?…"},
		{sqlshape.SQLite, `SELECT 'C:\' , name FROM t`, "SELECT ?, name FROM t"},
		{sqlshape.MySQL, `SELECT "a", 'b' FROM t`, "SELECT ?, ? FROM t"},
		{sqlshape.Unknown, "SELECT a FROM t /* x /* y */ z */", "SELECT a FROM t…"},
		{sqlshape.Postgres, "SELECT a # b FROM t", "SELECT a # b FROM t"},
		{sqlshape.MySQL, "SELECT 1 # comment 'x'\nFROM t", "SELECT ? FROM t"},
	}
	for _, c := range cases {
		if got := sqlshape.Shape(c.in, c.d, 1024); got != c.want {
			t.Errorf("Shape(%q, %d)\n got %q\nwant %q", c.in, c.d, got, c.want)
		}
	}
}

// TestSQLShape_B1_NoLeak lists the literal forms and their pitfalls. A secret inside one
// of them never survives.
func TestSQLShape_B1_NoLeak(t *testing.T) {
	cases := []struct {
		d  sqlshape.Dialect
		in string
	}{
		{sqlshape.Postgres, "SELECT $$SECRET$$"},
		{sqlshape.Postgres, "SELECT $fn$ it's SECRET $x$ $fn$"},
		{sqlshape.Postgres, "SELECT $tag$SECRET"}, // unterminated
		{sqlshape.Postgres, `SELECT E'\'SECRET'`},
		{sqlshape.Postgres, `SELECT E'\\', 'SECRET'`},
		{sqlshape.Postgres, `SELECT e'a\'b SECRET'`},
		{sqlshape.Postgres, "SELECT U&'d\\0061 SECRET'"},
		{sqlshape.Postgres, `SELECT 'C:\' , 'SECRET'`},
		{sqlshape.Postgres, "SELECT 'it''s SECRET'"},
		{sqlshape.Postgres, "SELECT 'SECRET"}, // unterminated
		{sqlshape.Postgres, "SELECT 1 /* SECRET"},
		{sqlshape.Postgres, "SELECT 1 /* a /* b */ SECRET */"},
		{sqlshape.Postgres, "/*traceparent='00-SECRET'*/ SELECT 1"},
		{sqlshape.MySQL, `SELECT "SECRET"`},
		{sqlshape.MySQL, `SELECT 'a\'SECRET'`},
		{sqlshape.MySQL, "SELECT 1 # SECRET"},
		{sqlshape.MySQL, "SELECT /*!50000 SECRET */ 1"},
		{sqlshape.MySQL, "SELECT _utf8mb4'SECRET'"},
		{sqlshape.SQLServer, "SELECT N'SECRET'"},
		{sqlshape.Unknown, `SELECT "SECRET" , 'a\'SECRET'`},
		{sqlshape.Unknown, "SELECT $q$SECRET$q$"},
		{sqlshape.Unknown, `SELECT 'a\''SECRET'`},
		{sqlshape.Unknown, "SELECT 1 # it's\n, SECRET, '"},
		{sqlshape.Unknown, "SELECT 1 --x'\n, SECRET, '"},
		{sqlshape.Unknown, "SELECT /* /* */ ' */ , SECRET, '"},
		{sqlshape.Unknown, "SELECT $a$ 'x $a$ SECRET'"},
		{sqlshape.MySQL, "SELECT 1--'\n, SECRET, '"},
		{sqlshape.SQLite, `SELECT "SECRET"`},
		{sqlshape.SQLServer, `SELECT "SECRET"`},
		{sqlshape.Postgres, "SELECT 12345678901234567890 AS SECRET_is_an_alias"},
	}
	for _, c := range cases {
		got := sqlshape.Shape(c.in, c.d, 1024)
		if strings.Contains(got, "SECRET") && !strings.Contains(c.in, "AS SECRET") {
			t.Errorf("leak: Shape(%q, %d) = %q", c.in, c.d, got)
		}
	}
}

// TestSQLShape_B1_Caps proves that the scan cap and the output cap both hold, and that a
// cut output stays valid UTF-8.
func TestSQLShape_B1_Caps(t *testing.T) {
	long := "SELECT " + strings.Repeat("é", 100) + " FROM t"
	got := sqlshape.Shape(long, sqlshape.Postgres, 20)
	if !strings.HasSuffix(got, sqlshape.Ellipsis) || !utf8.ValidString(got) {
		t.Fatalf("cap broken: %q", got)
	}
	if len(got) > 20+len(sqlshape.Ellipsis) {
		t.Fatalf("cap broken: %q is longer than 20 bytes plus the mark", got)
	}
	huge := "SELECT '" + strings.Repeat("x", sqlshape.MaxScan) + "'"
	if got := sqlshape.Shape(huge, sqlshape.Postgres, 1024); got != "SELECT ?"+sqlshape.Ellipsis {
		t.Fatalf("scan cap: %q", got)
	}
}

// TestSQLShape_B1_DefaultMax proves that a max of zero or less applies the default of 512
// bytes.
func TestSQLShape_B1_DefaultMax(t *testing.T) {
	long := "SELECT " + strings.Repeat("a", 700) + " FROM t"
	got := sqlshape.Shape(long, sqlshape.Postgres, 0)
	if len(got) > 512+len(sqlshape.Ellipsis) {
		t.Fatalf("the default cap did not apply: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, sqlshape.Ellipsis) {
		t.Fatalf("the default cap did not cut: %q", got[len(got)-8:])
	}
}

// TestSQLShape_B1_Operation proves that the operation is the first keyword, in upper case.
func TestSQLShape_B1_Operation(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM t WHERE id = ?":         "SELECT",
		"insert into t (a) values (?)":         "INSERT",
		"  UPDATE t SET a = ?":                 "UPDATE",
		"WITH x AS (SELECT 1) SELECT * FROM x": "WITH",
		"":                                     "",
		"?":                                    "?",
	}
	for shape, want := range cases {
		if got := sqlshape.Operation(shape); got != want {
			t.Errorf("Operation(%q) = %q, want %q", shape, got, want)
		}
	}
}
