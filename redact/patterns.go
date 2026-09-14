package redact

import (
	"regexp"
	"strings"
)

// builtinPattern is one built-in value pattern: a regex that finds candidate matches in
// a string value, and a masker that produces the replacement for one match (or returns
// the match unchanged when it turns out not to be a real hit, e.g. a non-Luhn number).
// enabledByDefault false means the pattern only runs when named in EnablePatterns.
type builtinPattern struct {
	name             string
	re               *regexp.Regexp
	masker           func(match string) string
	enabledByDefault bool
}

// allBuiltinPatterns is every built-in value pattern (SPEC-redact.md's pattern table).
// Patterns run in this order over one string, each seeing the previous one's output.
var allBuiltinPatterns = []builtinPattern{
	{"credit_card", regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`), maskCreditCard, true},
	{"email", regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), maskEmail, true},
	{"jwt", regexp.MustCompile(`\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), maskJWT, true},
	{"bearer", regexp.MustCompile(`(?i)\bBearer\s+\S+`), maskBearer, true},
	{"ipv4", reIPv4, maskIPv4, true},
	{"phone", rePhone, maskPhone, true},
	{"iban", reIBAN, maskIBAN, true},
	{"nik", reNIK, maskNIK, false},
}

// applyPatterns runs every active built-in pattern over s and returns the result. path is
// the field's full path from the event root, used only to exempt http.client_ip from the
// ipv4 pattern unless MaskClientIP was set.
func (r *Redactor) applyPatterns(s string, path []string) string {
	isClientIP := len(path) == 2 && path[0] == "http" && path[1] == "client_ip"
	for _, p := range r.patterns {
		if p.name == "ipv4" && isClientIP && !r.maskClientIP {
			continue
		}
		s = p.re.ReplaceAllStringFunc(s, p.masker)
	}
	return s
}

func maskCreditCard(match string) string {
	digits := digitsOf(match)
	if !luhnValid(digits) {
		return match
	}
	return "****" + digits[len(digits)-4:]
}

// luhnValid reports whether digits (ASCII '0'-'9') passes the Luhn checksum used by
// real card numbers, so a plain numeric id is not mistaken for a credit card.
func luhnValid(digits string) bool {
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
