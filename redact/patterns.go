package redact

import (
	"regexp"
	"strings"
)

// builtinPattern is one built-in value pattern: a regex that finds candidate matches in
// a string value, and a masker that produces the replacement for one match (or returns
// the match unchanged when it turns out not to be a real hit, e.g. a non-Luhn number).
type builtinPattern struct {
	name   string
	re     *regexp.Regexp
	masker func(match string) string
}

// defaultPatterns are on by default (SPEC-redact.md's pattern table). Order matters only
// in that each runs over the previous one's output; the patterns below do not overlap in
// practice.
var defaultPatterns = []builtinPattern{
	{"credit_card", regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`), maskCreditCard},
	{"email", regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), maskEmail},
	{"jwt", regexp.MustCompile(`\b[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`), maskJWT},
	{"bearer", regexp.MustCompile(`(?i)\bBearer\s+\S+`), maskBearer},
}

// applyPatterns runs every active built-in pattern over s and returns the result.
func (r *Redactor) applyPatterns(s string) string {
	for _, p := range r.patterns {
		s = p.re.ReplaceAllStringFunc(s, p.masker)
	}
	return s
}

func maskCreditCard(match string) string {
	digits := make([]byte, 0, len(match))
	for i := 0; i < len(match); i++ {
		if match[i] >= '0' && match[i] <= '9' {
			digits = append(digits, match[i])
		}
	}
	if !luhnValid(digits) {
		return match
	}
	return "****" + string(digits[len(digits)-4:])
}

// luhnValid reports whether digits (ASCII '0'-'9') passes the Luhn checksum used by
// real card numbers, so a plain numeric id is not mistaken for a credit card.
func luhnValid(digits []byte) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

func maskEmail(match string) string {
	at := strings.IndexByte(match, '@')
	if at < 1 {
		return match
	}
	local, domain := match[:at], match[at+1:]
	tld := domain
	if dot := strings.LastIndexByte(domain, '.'); dot >= 0 {
		tld = domain[dot+1:]
	}
	return local[:1] + "***@***." + tld
}

func maskJWT(match string) string {
	n := min(len(match), 3)
	return match[:n] + "***.***"
}

func maskBearer(string) string {
	return "Bearer ***"
}
