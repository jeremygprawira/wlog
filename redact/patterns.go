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
	// prefilter is a cheap necessary-but-not-sufficient check run before the regex.
	// Go's regexp backtracks on these patterns, and that cost dwarfs a byte scan, so
	// skipping the regex on strings that plainly can't match (e.g. no "@" for email)
	// is a real win, not premature optimization: measured 76% of Apply's CPU before
	// this existed. nil means always try the regex (used by user-supplied patterns,
	// which have no safe cheap precondition to infer).
	prefilter func(string) bool
	// custom marks a pattern that AddPatterns added, so a later With keeps it.
	custom bool
}

// valueMasker adapts a simple func(matchedText string) string into the func(Match)
// string shape every pattern uses, for the built-ins that only look at the match text.
func valueMasker(f func(string) string) func(Match) string {
	return func(m Match) string { return f(m.Value) }
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

func hasUpper(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}

func hasBearer(s string) bool {
	return strings.Contains(s, "Bearer") || strings.Contains(strings.ToLower(s), "bearer")
}

// allBuiltinPatterns is every built-in value pattern (SPEC-redact.md's pattern table).
// Patterns run in this order over one string, each seeing the previous one's output.
var allBuiltinPatterns = []builtinPattern{
	{"credit_card", regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`), valueMasker(maskCreditCard), true, hasDigit, false},
	{"email", regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`), valueMasker(maskEmail), true, func(s string) bool { return strings.Contains(s, "@") }, false},
	{"jwt", regexp.MustCompile(`\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), valueMasker(maskJWT), true, func(s string) bool { return strings.Count(s, ".") >= 2 }, false},
	{"bearer", regexp.MustCompile(`(?i)\bBearer\s+\S+`), valueMasker(maskBearer), true, hasBearer, false},
	{"url_credentials", regexp.MustCompile(`([a-z][a-z0-9+.\-]*://[^:/@\s]+:)([^@/\s]+)(@)`), valueMasker(maskURLPassword), true, func(s string) bool { return strings.Contains(s, "://") }, false},
	{"url_query_secret", reQuerySecret, valueMasker(maskQuerySecret), true, func(s string) bool { return strings.Contains(s, "=") }, false},
	{"basic_auth", regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=]{8,}`), valueMasker(maskBasicAuth), true, hasBasic, false},
	{"api_key_prefix", regexp.MustCompile(`\b(?:sk_live_|sk_test_|AKIA|ghp_|xoxb-|glpat-)[A-Za-z0-9_\-]{4,}`), valueMasker(maskAPIKey), true, hasAPIKeyPrefix, false},
	{"ipv4", reIPv4, valueMasker(maskIPv4KeepLead), true, func(s string) bool { return strings.Count(s, ".") >= 3 }, false},
	{"phone", rePhone, valueMasker(maskPhone), true, hasDigit, false},
	{"iban", reIBAN, valueMasker(maskIBAN), true, hasUpper, false},
	{"nik", reNIK, valueMasker(maskNIK), false, hasDigit, false},
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
		if p.prefilter != nil && !p.prefilter(s) {
			continue
		}
		s = applyRegex(s, p.re, path, p.masker, r.replacement)
	}
	return s
}

// applyRegex replaces every match of re in s with maskSafe(masker, match), building the
// Match{Path, Key, Value, Groups} each masker sees. fallback is used if masker panics.
func applyRegex(s string, re *regexp.Regexp, path []string, masker func(Match) string, fallback string) string {
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
		b.WriteString(maskSafe(masker, match, fallback))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// maskSafe calls masker and recovers a panic as fallback, so one bad custom pattern can
// never crash the request that logged through it.
func maskSafe(masker func(Match) string, m Match, fallback string) (out string) {
	defer func() {
		if recover() != nil {
			out = fallback
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

// reQuerySecret matches a query parameter whose name names a secret, so the value
// is masked while the harmless parameters of the same URL stay readable.
var reQuerySecret = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|access[_-]?key|auth|authorization|credential|dsn|key|passphrase|password|secret|session|signature|token)[^=&\s]*=)([^&\s]+)`)

// maskIPv4KeepLead masks the address and keeps the guard character that the
// pattern captured, because that character is what excludes a version string.
func maskIPv4KeepLead(match string) string {
	start := 0
	for start < len(match) && (match[start] < '0' || match[start] > '9') {
		start++
	}
	if start == 0 {
		return maskIPv4(match)
	}
	return match[:start] + maskIPv4(match[start:])
}

// maskURLPassword keeps the scheme, the user, and the host, and masks only the
// password of a URL, so a reader still learns which backend answered.
func maskURLPassword(match string) string {
	i := strings.Index(match, "://")
	if i < 0 {
		return "[REDACTED]"
	}
	rest := match[i+3:]
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return "[REDACTED]"
	}
	creds := rest[:at]
	colon := strings.Index(creds, ":")
	if colon < 0 {
		return "[REDACTED]"
	}
	return match[:i+3] + creds[:colon] + ":[REDACTED]" + rest[at:]
}

// maskQuerySecret masks the value of a query parameter and keeps its name.
func maskQuerySecret(match string) string {
	eq := strings.Index(match, "=")
	if eq < 0 {
		return "[REDACTED]"
	}
	return match[:eq+1] + "[REDACTED]"
}

// maskBasicAuth keeps the scheme, because the header still says what it is.
func maskBasicAuth(string) string { return "Basic [REDACTED]" }

// maskAPIKey keeps the prefix, so an operator can tell which provider leaked a key.
func maskAPIKey(match string) string {
	for _, prefix := range []string{"sk_live_", "sk_test_", "AKIA", "ghp_", "xoxb-", "glpat-"} {
		if strings.HasPrefix(match, prefix) {
			return prefix + "[REDACTED]"
		}
	}
	return "[REDACTED]"
}

// hasBasic reports whether a string may hold a Basic header.
func hasBasic(s string) bool {
	return strings.Contains(s, "Basic") || strings.Contains(s, "basic")
}

// hasAPIKeyPrefix reports whether a string may start a known key.
func hasAPIKeyPrefix(s string) bool {
	for _, prefix := range []string{"sk_live_", "sk_test_", "AKIA", "ghp_", "xoxb-", "glpat-"} {
		if strings.Contains(s, prefix) {
			return true
		}
	}
	return false
}
