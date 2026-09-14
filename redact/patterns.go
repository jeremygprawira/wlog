package redact

import (
	"regexp"
	"strings"
)

// builtinPattern is one built-in value pattern: a regex that finds candidate matches in
// a string value, and a masker that produces the replacement for one match (or returns
// the original value unchanged when it turns out not to be a real hit, e.g. a non-Luhn
// number). enabledByDefault false means the pattern only runs when named in
// EnablePatterns. A masker that panics is treated as "[REDACTED]" (see applyPatterns).
type builtinPattern struct {
	name             string
	re               *regexp.Regexp
	masker           func(Match) string
	enabledByDefault bool
}

// valueMasker adapts a simple func(matchedText string) string into the func(Match)
// string shape every pattern uses, for the built-ins that only look at the match text.
func valueMasker(f func(string) string) func(Match) string {
	return func(m Match) string { return f(m.Value) }
}

// allBuiltinPatterns is every built-in value pattern (SPEC-redact.md's pattern table).
// Patterns run in this order over one string, each seeing the previous one's output.
var allBuiltinPatterns = []builtinPattern{
	{"credit_card", regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`), valueMasker(maskCreditCard), true},
	{"email", regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), valueMasker(maskEmail), true},
	{"jwt", regexp.MustCompile(`\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), valueMasker(maskJWT), true},
	{"bearer", regexp.MustCompile(`(?i)\bBearer\s+\S+`), valueMasker(maskBearer), true},
	{"ipv4", reIPv4, valueMasker(maskIPv4), true},
	{"phone", rePhone, valueMasker(maskPhone), true},
	{"iban", reIBAN, valueMasker(maskIBAN), true},
	{"nik", reNIK, valueMasker(maskNIK), false},
}

// applyPatterns runs every active pattern over s and returns the result. path is the
// field's full path from the event root: it becomes Match.Path/Key, and it is used to
// exempt http.client_ip from the ipv4 pattern unless MaskClientIP was set.
func (r *Redactor) applyPatterns(s string, path []string) string {
	isClientIP := len(path) == 2 && path[0] == "http" && path[1] == "client_ip"
	for _, p := range r.patterns {
		if p.name == "ipv4" && isClientIP && !r.maskClientIP {
			continue
		}
		s = applyRegex(s, p.re, path, p.masker)
	}
	return s
}

// applyRegex replaces every match of re in s with maskSafe(masker, match), building the
// Match{Path, Key, Value, Groups} each masker sees.
func applyRegex(s string, re *regexp.Regexp, path []string, masker func(Match) string) string {
	idxs := re.FindAllStringSubmatchIndex(s, -1)
	if idxs == nil {
		return s
	}
	pathStr := strings.Join(path, ".")
	key := ""
	if len(path) > 0 {
		key = path[len(path)-1]
	}

	var b strings.Builder
	last := 0
	for _, m := range idxs {
		b.WriteString(s[last:m[0]])
		var groups []string
		for g := 2; g < len(m); g += 2 {
			if m[g] < 0 {
				groups = append(groups, "")
				continue
			}
			groups = append(groups, s[m[g]:m[g+1]])
		}
		match := Match{Path: pathStr, Key: key, Value: s[m[0]:m[1]], Groups: groups}
		b.WriteString(maskSafe(masker, match))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// maskSafe calls masker and recovers a panic as "[REDACTED]", so one bad custom
// pattern can never crash the request that logged through it.
func maskSafe(masker func(Match) string, m Match) (out string) {
	defer func() {
		if recover() != nil {
			out = "[REDACTED]"
		}
	}()
	return masker(m)
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
