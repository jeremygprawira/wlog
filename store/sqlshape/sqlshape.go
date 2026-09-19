// Package sqlshape turns SQL text into a statement shape, with the standard library only.
//
// Read top to bottom: Shape scans one query in one pass and writes the shape. The scan
// copies identifiers, keywords, operators, and punctuation, and it writes "?" for every
// string, number, and positional placeholder. It drops comments. A construct whose meaning
// depends on the dialect or a server setting stops the scan, so a doubt removes text and
// never adds it. Operation reads the first keyword of a shape for the call record.
//
// The function is a safety tool. The shape must never hold a literal, because a literal
// holds a user value, a token, or a password.
package sqlshape

import (
	"strings"
	"unicode/utf8"
)

// Dialect selects the lexing rules that differ between databases.
type Dialect uint8

const (
	// Unknown applies the union of the rules and stops on any conflict.
	Unknown Dialect = iota
	// Postgres reads dollar quotes and E'...' escapes.
	Postgres
	// MySQL reads backticks, # comments, and backslash escapes.
	MySQL
	// SQLite reads square brackets and plain backslashes.
	SQLite
	// SQLServer reads square brackets and plain backslashes.
	SQLServer
)

const (
	// MaxScan bounds the scan at 64 KiB of input. Input past it is dropped.
	MaxScan = 64 << 10
	// Ellipsis marks a shape that lost input.
	Ellipsis = "…"
	// DefaultMax caps the output at 512 bytes when a caller passes no cap.
	DefaultMax = 512
)

// Shape returns the statement shape of a query, capped at max bytes plus Ellipsis. A max of
// zero or less applies DefaultMax.
func Shape(query string, d Dialect, max int) string {
	if max <= 0 {
		max = DefaultMax
	}
	s := &shaper{}
	cut := len(query) > MaxScan
	if cut {
		query = query[:MaxScan]
	}
	if !s.scan(query, d) {
		cut = true
	}
	return finish(s.b.String(), max, cut)
}

// Operation returns the first keyword of a statement shape, in upper case, such as SELECT
// or INSERT. It answers an empty string for a shape that holds no word.
func Operation(shape string) string {
	fields := strings.Fields(shape)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

// shaper builds one shape and tracks the space between tokens.
type shaper struct {
	b     strings.Builder
	space bool
}

// emit writes one token, and the space before it when the shape needs one.
func (s *shaper) emit(tok string) {
	if s.space && s.b.Len() > 0 && tok != ")" && tok != "," && s.b.String()[s.b.Len()-1] != '(' {
		s.b.WriteByte(' ')
	}
	s.b.WriteString(tok)
	s.space = tok == ","
}

// scan walks one query and returns false when it stopped early.
func (s *shaper) scan(q string, d Dialect) bool {
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case isSpace(c):
			s.space, i = true, i+1

		case c == '\'':
			// A backslash means an escape under MySQL default mode or under Postgres
			// with standard_conforming_strings off, and a plain byte otherwise.
			end, ok := endQuote(q, i+1, '\'', mode(d == SQLite || d == SQLServer))
			s.emit("?")
			if !ok {
				return false
			}
			i = end

		case c == '"' && d == Postgres, c == '`', c == '[' && (d == SQLServer || d == SQLite):
			closer := c
			if c == '[' {
				closer = ']'
			}
			end, ok := endQuote(q, i+1, closer, bsPlain)
			if !ok {
				return false
			}
			s.emit(q[i:end])
			i = end

		case c == '"':
			// MySQL reads this as a string unless ANSI_QUOTES is set. SQLite and SQL
			// Server may also read it as a string.
			end, ok := endQuote(q, i+1, '"', mode(d == SQLite || d == SQLServer))
			s.emit("?")
			if !ok {
				return false
			}
			i = end

		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			if d == MySQL && i+2 < len(q) && !isSpace(q[i+2]) {
				s.emit("-") // MySQL needs whitespace after -- for a comment
				i++
				break
			}
			end := lineEnd(q, i)
			if d == Unknown && strings.ContainsAny(q[i:end], `'"`) {
				return false
			}
			s.space, i = true, end

		case c == '#' && (d == MySQL || d == Unknown):
			end := lineEnd(q, i)
			if d == Unknown && strings.ContainsAny(q[i:end], `'"`) {
				return false // # is an operator in Postgres
			}
			s.space, i = true, end

		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			nest := d == Postgres || d == SQLServer || d == Unknown
			depth, j := 1, i+2
			for j < len(q) && depth > 0 {
				switch {
				case q[j] == '*' && j+1 < len(q) && q[j+1] == '/':
					depth, j = depth-1, j+2
				case q[j] == '/' && j+1 < len(q) && q[j+1] == '*' && nest:
					if d == Unknown {
						return false // nesting differs between databases
					}
					depth, j = depth+1, j+2
				default:
					j++
				}
			}
			if depth > 0 {
				return false
			}
			s.space, i = true, j

		case c == '$' && (d == Postgres || d == Unknown) && (i == 0 || !isIdent(q[i-1])):
			j := i + 1
			if j < len(q) && isDigit(q[j]) { // $1 placeholder
				for j < len(q) && isDigit(q[j]) {
					j++
				}
				s.emit("?")
				i = j
				break
			}
			for j < len(q) && q[j] != '$' && isIdent(q[j]) {
				j++
			}
			if j >= len(q) || q[j] != '$' {
				s.emit("$")
				i++
				break
			}
			tag := q[i : j+1] // $tag$ ... $tag$
			k := strings.Index(q[j+1:], tag)
			s.emit("?")
			if k < 0 || d == Unknown && strings.ContainsAny(q[j+1:j+1+k], `'"`) {
				return false
			}
			i = j + 1 + k + len(tag)

		case c == '?':
			j := i + 1
			for j < len(q) && isDigit(q[j]) { // SQLite ?NNN
				j++
			}
			s.emit("?")
			i = j

		case isDigit(c) || c == '.' && i+1 < len(q) && isDigit(q[i+1]) && (i == 0 || !isIdent(q[i-1])):
			j := scanNumber(q, i)
			if j < len(q) && isIdentStart(q[j]) { // identifiers like 2fa_codes
				for j < len(q) && isIdent(q[j]) {
					j++
				}
				s.emit(q[i:j])
			} else {
				s.emit("?")
			}
			i = j

		case isIdentStart(c):
			j := i + 1
			for j < len(q) && isIdent(q[j]) {
				j++
			}
			word := q[i:j]
			if j < len(q) && q[j] == '\'' {
				// E'..' N'..' X'..' B'..' _utf8mb4'..' drop the prefix. DATE'..' keeps it.
				bs := mode(d == SQLite || d == SQLServer)
				if d == Postgres && (word == "E" || word == "e") {
					bs = bsEscape
				}
				end, ok := endQuote(q, j+1, '\'', bs)
				if len(word) > 1 && word[0] != '_' {
					s.emit(word)
					s.space = true
				}
				s.emit("?")
				if !ok {
					return false
				}
				i = end
				break
			}
			s.emit(word)
			i = j

		default:
			_, w := utf8.DecodeRuneInString(q[i:])
			s.emit(q[i : i+w])
			i += w
		}
	}
	return true
}

// Backslash modes for endQuote.
const (
	bsPlain  = iota // a backslash is a plain byte: SQLite, SQL Server, identifiers
	bsEscape        // a backslash escapes the next byte: Postgres E'...'
	bsStop          // the meaning depends on server settings, so stop
)

// endQuote returns the index just past the closing quote. A doubled quote is an escaped
// quote. It reports false when the quote is unterminated or ambiguous.
func endQuote(q string, i int, quote byte, bs int) (int, bool) {
	for ; i < len(q); i++ {
		switch q[i] {
		case '\\':
			switch bs {
			case bsStop:
				return len(q), false
			case bsEscape:
				i++
			}
		case quote:
			if i+1 < len(q) && q[i+1] == quote {
				i++
				continue
			}
			return i + 1, true
		}
	}
	return len(q), false
}

// mode returns the backslash mode of one dialect.
func mode(plain bool) int {
	if plain {
		return bsPlain
	}
	return bsStop
}

// lineEnd returns the index of the end of the line that starts at i.
func lineEnd(q string, i int) int {
	if n := strings.IndexByte(q[i:], '\n'); n >= 0 {
		return i + n
	}
	return len(q)
}

// scanNumber returns the index just past one number.
func scanNumber(q string, i int) int {
	if q[i] == '0' && i+1 < len(q) && strings.IndexByte("xXbBoO", q[i+1]) >= 0 {
		j := i + 2
		for j < len(q) && (isDigit(q[j]) || q[j] == '_' || q[j]|0x20 >= 'a' && q[j]|0x20 <= 'f') {
			j++
		}
		return j
	}
	j := i
	for j < len(q) && (isDigit(q[j]) || q[j] == '_' || q[j] == '.') {
		j++
	}
	if j < len(q) && q[j]|0x20 == 'e' {
		k := j + 1
		if k < len(q) && (q[k] == '+' || q[k] == '-') {
			k++
		}
		if k < len(q) && isDigit(q[k]) {
			for k < len(q) && isDigit(q[k]) {
				k++
			}
			j = k
		}
	}
	return j
}

// finish collapses literal lists, fixes UTF-8, and applies the output cap.
func finish(s string, max int, cut bool) string {
	s = collapse(s, "(?", ", ?", ")")
	s = collapse(s, "(?)", ", (?)", "")
	s = strings.ToValidUTF8(s, "�")
	if len(s) > max {
		n := max
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s, cut = s[:n], true
	}
	if cut {
		s += Ellipsis
	}
	return s
}

// collapse rewrites open rep rep ... close into open close, so "(?, ?, ?)" becomes "(?)".
func collapse(s, open, rep, close string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, open+rep)
		if i < 0 {
			if b.Len() == 0 {
				return s
			}
			b.WriteString(s)
			return b.String()
		}
		j := i + len(open)
		for strings.HasPrefix(s[j:], rep) {
			j += len(rep)
		}
		if !strings.HasPrefix(s[j:], close) {
			b.WriteString(s[:i+len(open)])
			s = s[i+len(open):]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString(open)
		b.WriteString(close)
		s = s[j+len(close):]
	}
}

// isIdent reports whether one byte continues an identifier.
func isIdent(c byte) bool {
	return c == '_' || c == '$' || c >= 0x80 || c|0x20 >= 'a' && c|0x20 <= 'z' || isDigit(c)
}

// isIdentStart reports whether one byte starts an identifier.
func isIdentStart(c byte) bool {
	return c == '_' || c >= 0x80 || c|0x20 >= 'a' && c|0x20 <= 'z'
}

// isDigit reports whether one byte is a decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isSpace reports whether one byte is whitespace.
func isSpace(c byte) bool { return c <= ' ' }
