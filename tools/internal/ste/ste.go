// Package ste finds the mechanical Simple English mistakes in a text.
//
// The rules come from the Simple English skill, which follows ASD-STE100. The
// package is a port of the Python linter that lives beside that skill, so a
// document that passes there passes here, with the same categories and the same
// counts. It reports the mistakes that a regular expression can catch:
//
//	sentence_over_limit  a sentence longer than the limit of its register
//	trailing_condition   a sentence that names the condition after the command
//	contraction          a shortened verb form, such as "it's"
//	banned_modal         should, would, may, might, or could
//	perfect_tense        "has been" or "have <verb>ed"
//	ing_clause           ", making ..." and the like
//	semicolon            a semicolon
//	em_dash              an em-dash, or a spaced hyphen between two words
//	latin_abbrev         e.g., i.e., or etc.
//	slop_word            a word from the measured list of filler terms
//	synonym_rotation     check, verify, confirm, validate, or ensure in one text
//
// Known ceiling: this is a regular expression pass, not a grammar parser. It
// counts no passive voice and no part of speech, and it can read a sentence
// bound in unusual markdown. The counts compare one text with another through
// the same version. They are not a compliance verdict.
package ste

import (
	_ "embed"
	"regexp"
	"sort"
	"strings"
)

// Register names the sentence limit that a passage must meet. Procedural text
// tells the reader what to do, and descriptive text explains.
type Register string

// The two registers of a document.
const (
	Procedural  Register = "procedural"
	Descriptive Register = "descriptive"
)

// Limits holds the sentence limit in words for each register.
var Limits = map[Register]int{Procedural: 20, Descriptive: 25}

//go:embed slop.tsv
var slopTSV string

var (
	bannedModals = regexp.MustCompile(`(?i)\b(should|would|may|might|could)\b`)
	perfectTense = regexp.MustCompile(`(?i)\b(has|have|had)\s+been\b|\b(has|have)\s+\w+ed\b`)
	contraction  = regexp.MustCompile(`(?i)\b\w+(n't|'ll|'re|'ve|'d)\b|\bit's\b|\byou're\b`)
	ingClause    = regexp.MustCompile(`(?i),\s*(mak|allow|enabl|ensur|highlight|creat|provid|offer|help|reduc|improv|lead|caus|result)ing\b`)
	latinAbbrev  = regexp.MustCompile(`(?i)(?:e\.g\.|i\.e\.|etc\.?)(?:[\s,)]|$)`)
	trailingCond = regexp.MustCompile(`(?i)\s(if|when)\s`)
	conditionLed = regexp.MustCompile(`(?i)^(if|when)\b`)
	fencedCode   = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	// htmlComment covers a marker such as <!-- snippet:sketch -->, which is invisible in a
	// rendered document and obeys no prose rule.
	htmlComment = regexp.MustCompile("(?s)<!--.*?-->")
	codeSpan    = regexp.MustCompile("`[^`\n]+`")
	heading     = regexp.MustCompile(`(?m)^#+\s.*$`)
	url         = regexp.MustCompile(`https?://\S+`)
	tableSep    = regexp.MustCompile(`(?m)^\s*\|[\s:|-]+\|\s*$`)
	tableRow    = regexp.MustCompile(`(?m)^\s*\|.*\|\s*$`)
	listItem    = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+[.)])\s+.*$`)

	// One term, one meaning: a document that names one thing two ways is
	// harder to read, so the second name counts as a rotation.
	rotationSets = []struct {
		name string
		rx   *regexp.Regexp
	}{
		{"check-verify", regexp.MustCompile(`(?i)\b(check|verify|confirm|validate|ensure)\w*\b`)},
		{"config-settings", regexp.MustCompile(`(?i)\b(config|configuration|settings)\b`)},
	}

	slop = slopPattern()
)

// slopPattern builds the filler-word list from slop.tsv, the measured list of
// terms that eight or more of 122 published ban lists name, plus the core list
// that the skill adds. A space inside a term matches any run of whitespace, so a
// term that wraps across a line still counts.
func slopPattern() *regexp.Regexp {
	terms := []string{
		`simply`, `seamlessly`, `effortlessly`, `robust`, `leverag\w*`, `utiliz\w*`,
		`comprehensive`, `powerful`, `blazingly`, `streamlin\w*`, `facilitat\w*`,
		`performant`, `plethora`, `myriad`, `delve`, `crucial`, `pivotal`,
	}
	for _, line := range strings.Split(slopTSV, "\n") {
		term := strings.ToLower(strings.TrimSpace(strings.SplitN(line, "\t", 2)[0]))
		if term == "" {
			continue
		}
		terms = append(terms, strings.ReplaceAll(regexp.QuoteMeta(term), `\ `, `\s+`)+`\w*`)
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(terms, "|") + `)\b`)
}

// Hit is one mistake, with the text that caused it and the line to read.
type Hit struct {
	Category string // the rule name, such as "banned_modal"
	Text     string // the matched text, or the sentence for a length hit
	Line     int    // the line number in the text as given
}

// Report is the result of one lint pass.
type Report struct {
	Register  Register
	Words     int
	Sentences int
	Hits      []Hit
}

// Total returns the number of mistakes.
func (r Report) Total() int { return len(r.Hits) }

// Counts returns the number of hits per category.
func (r Report) Counts() map[string]int {
	counts := map[string]int{}
	for _, hit := range r.Hits {
		counts[hit.Category]++
	}
	return counts
}

// Lint finds every mistake in a markdown or plain text passage and returns the
// hits in line order.
//
// The limit comes from the register: 20 words for procedural text, 25 for
// descriptive text. Fenced code, inline code, headings, and URLs are removed
// first, because code and titles obey other rules.
func Lint(text string, register Register) Report {
	limit := Limits[register]
	if limit == 0 {
		register, limit = Descriptive, Limits[Descriptive]
	}

	body := stripCode(text)
	sentences := splitSentences(body)
	var hits []Hit

	// A sentence may appear twice in one file, so the search walks forward: each copy keeps its
	// own line number.
	cursor := 0
	for _, sentence := range sentences {
		at := strings.Index(body[cursor:], sentence)
		if at < 0 {
			at = 0
		} else {
			at += cursor
			cursor = at + len(sentence)
		}
		if len(strings.Fields(sentence)) > limit {
			hits = append(hits, Hit{"sentence_over_limit", clip(sentence), lineOf(body, at)})
		}
		if trailingCondition(sentence) {
			hits = append(hits, Hit{"trailing_condition", clip(sentence), lineOf(body, at)})
		}
	}
	for _, rule := range []struct {
		name string
		rx   *regexp.Regexp
	}{
		{"contraction", contraction},
		{"banned_modal", bannedModals},
		{"perfect_tense", perfectTense},
		{"ing_clause", ingClause},
		{"latin_abbrev", latinAbbrev},
		{"slop_word", slop},
	} {
		for _, m := range rule.rx.FindAllStringIndex(body, -1) {
			hits = append(hits, Hit{rule.name, strings.TrimRight(body[m[0]:m[1]], " \t\n\r,)"), lineOf(body, m[0])})
		}
	}
	for _, at := range allIndexes(body, ';') {
		hits = append(hits, Hit{"semicolon", ";", lineOf(body, at)})
	}
	for _, dash := range dashes(body) {
		hits = append(hits, Hit{"em_dash", dash.text, lineOf(body, dash.at)})
	}
	hits = append(hits, rotations(body)...)

	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Line < hits[j].Line })
	return Report{Register: register, Words: len(strings.Fields(body)), Sentences: len(sentences), Hits: hits}
}

// rotations reports a text that names one thing two ways. The first name is
// correct, and every name after it counts.
func rotations(body string) []Hit {
	var hits []Hit
	for _, set := range rotationSets {
		seen := map[string]bool{}
		var names []string
		for _, m := range set.rx.FindAllStringSubmatchIndex(body, -1) {
			stem := strings.TrimRight(strings.ToLower(body[m[2]:m[3]]), "s")
			if !seen[stem] {
				seen[stem] = true
				names = append(names, body[m[0]:m[1]])
			}
		}
		if len(names) < 2 {
			continue
		}
		for _, name := range names[1:] {
			hits = append(hits, Hit{"synonym_rotation", name + " (" + set.name + ")", lineOf(body, strings.Index(body, name))})
		}
	}
	return hits
}

// dash is one dash that joins two parts of a sentence, with the text of the
// dash so a report can show it.
type dash struct {
	at   int
	text string
}

// dashes returns every dash that joins two parts of a sentence.
//
// Go regular expressions have no lookaround, so this rule walks the text: the
// em-dash always counts, the en-dash counts unless a digit sits on both sides,
// and a spaced hyphen counts only between two words of two characters or more.
func dashes(body string) []dash {
	var out []dash
	for i := 0; i < len(body); {
		switch {
		case strings.HasPrefix(body[i:], "—"):
			out = append(out, dash{i, "—"})
			i += len("—")
		case strings.HasPrefix(body[i:], "–"):
			if !digitBefore(body, i) && !digitAfter(body, i) {
				out = append(out, dash{i, "–"})
			}
			i += len("–")
		case strings.HasPrefix(body[i:], " -- "):
			out = append(out, dash{i + 1, "--"})
			i += len(" -- ")
		case strings.HasPrefix(body[i:], " - "):
			// The two characters before and after the spaced hyphen must both
			// be word characters, so a range or a flag never counts.
			if wordAtLeastTwo(body, i-1, -1) && wordAtLeastTwo(body, i+3, 1) {
				out = append(out, dash{i + 1, "-"})
			}
			i += len(" - ")
		default:
			i++
		}
	}
	return out
}

// wordAtLeastTwo reports whether two characters that are neither a space nor a
// digit sit on one side of a dash. start is the first character, and step walks
// away from the dash.
func wordAtLeastTwo(body string, start, step int) bool {
	count, i := 0, start
	for i >= 0 && i < len(body) && count < 2 {
		c := body[i]
		if isSpace(c) || isDigit(c) {
			return false
		}
		count++
		i += step
	}
	return count == 2
}

// digitBefore reports whether a digit sits just before an offset.
func digitBefore(body string, at int) bool {
	return at >= 1 && isDigit(body[at-1])
}

// digitAfter reports whether a digit sits just after an offset.
func digitAfter(body string, at int) bool {
	return at+1 < len(body) && isDigit(body[at+1])
}

// isSpace reports whether a byte is a space or a line break.
func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// isDigit reports whether a byte is an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// stripCode removes the parts of a text that obey other rules: fenced code,
// inline code, headings, and URLs. Every newline stays, so a line number in the
// stripped body is the same line number in the original text.
func stripCode(text string) string {
	// A fenced block becomes as many line breaks as it held, so a hit below a block still names
	// the line a reader must open.
	body := fencedCode.ReplaceAllStringFunc(text, func(block string) string {
		return strings.Repeat("\n", strings.Count(block, "\n"))
	})
	body = htmlComment.ReplaceAllStringFunc(body, func(comment string) string {
		return strings.Repeat("\n", strings.Count(comment, "\n"))
	})
	body = codeSpan.ReplaceAllString(body, " CODESPAN ")
	body = heading.ReplaceAllString(body, " ")
	body = url.ReplaceAllString(body, " URL ")
	body = tableSep.ReplaceAllString(body, " ")
	return body
}

// splitSentences breaks a text into sentences of two words or more.
//
// Each list item and each table cell becomes its own sentence, so a bullet list
// or a table row never counts as one long sentence.
func splitSentences(text string) []string {
	body := listItem.ReplaceAllStringFunc(text, func(line string) string {
		item := strings.TrimSpace(regexp.MustCompile(`^([-*+]|\d+[.)])\s+`).ReplaceAllString(strings.TrimSpace(line), ""))
		if strings.ContainsAny(rightmost(item), ".!?:") {
			return item + " "
		}
		return item + ". "
	})
	body = tableRow.ReplaceAllStringFunc(body, func(row string) string {
		cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
		kept := make([]string, 0, len(cells))
		for _, cell := range cells {
			if cell = strings.TrimSpace(cell); cell != "" {
				kept = append(kept, cell)
			}
		}
		return strings.Join(kept, ". ") + ". "
	})

	var sentences []string
	for _, part := range splitAfterPunctuation(body) {
		if part = strings.TrimSpace(part); len(strings.Fields(part)) >= 2 {
			sentences = append(sentences, part)
		}
	}
	return sentences
}

// splitAfterPunctuation splits a text into sentences. A full stop, a colon, a
// question mark, or an exclamation mark ends a sentence when whitespace follows
// it, and the mark stays with the sentence it closes.
//
// The walk replaces the lookbehind form of a regular expression, which Go does
// not have.
func splitAfterPunctuation(body string) []string {
	var out []string
	start := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '.', '!', '?', ':':
			j := i + 1
			for j < len(body) && isSpace(body[j]) {
				j++
			}
			if j > i+1 {
				out = append(out, body[start:i+1])
				start = j
			}
		}
	}
	return append(out, body[start:])
}

// rightmost returns the last character of a string, or the empty string.
func rightmost(s string) string {
	if s == "" {
		return ""
	}
	return s[len(s)-1:]
}

// trailingCondition reports whether a sentence names its condition after the
// command, as in "Restart the service if the log grows".
//
// A sentence that opens with its condition is correct, so it passes. The four
// characters before the condition must sit on the same line, which keeps a
// heading or a blank line above "If ..." from reading as a trailing condition.
func trailingCondition(sentence string) bool {
	m := trailingCond.FindStringIndex(sentence)
	if m == nil {
		return false
	}
	lineStart := strings.LastIndex(sentence[:m[0]], "\n") + 1
	if m[0]-lineStart < 4 {
		return false
	}
	return !conditionLed.MatchString(sentence)
}

// lineOf returns the 1-based line number of an offset.
func lineOf(body string, offset int) int {
	if offset < 0 {
		return 1
	}
	return strings.Count(body[:offset], "\n") + 1
}

// allIndexes returns every offset of one byte.
func allIndexes(body string, b byte) []int {
	var out []int
	for i := 0; i < len(body); i++ {
		if body[i] == b {
			out = append(out, i)
		}
	}
	return out
}

// clip shortens a long sentence for a report line.
func clip(sentence string) string {
	if len(sentence) <= 80 {
		return sentence
	}
	return sentence[:80] + "…"
}
