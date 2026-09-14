// Package redact scrubs sensitive data from a wide event before it reaches any sink.
//
// Read top to bottom: New compiles options into an immutable *Redactor; Apply walks one
// event snapshot and masks it in place. A *Redactor never changes after New returns — to
// change the denylist, derive a new one with With and swap it on the logger.
package redact

import (
	"strings"
	"unicode"
)

// tokenize splits a key into lowercase word tokens on separators (_ - . space) and
// camelCase/acronym boundaries, so key matching compares whole words, never substrings
// ("auth" must not match "author"; "HTTPAuthToken" becomes "http", "auth", "token").
func tokenize(key string) []string {
	var segments []string
	var cur []rune
	for _, r := range key {
		if r == '_' || r == '-' || r == '.' || r == ' ' {
			if len(cur) > 0 {
				segments = append(segments, string(cur))
				cur = nil
			}
			continue
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		segments = append(segments, string(cur))
	}

	var tokens []string
	for _, seg := range segments {
		tokens = append(tokens, splitCamel(seg)...)
	}
	for i, t := range tokens {
		tokens[i] = strings.ToLower(t)
	}
	return tokens
}

// splitCamel splits one separator-free segment on camelCase and acronym boundaries:
// lower-to-upper ("accessToken") and an upper run followed by lower ("HTTPAuth" splits
// before "Auth").
func splitCamel(seg string) []string {
	runes := []rune(seg)
	if len(runes) == 0 {
		return nil
	}
	var tokens []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		boundary := !unicode.IsUpper(prev) && unicode.IsUpper(cur)
		if !boundary && unicode.IsUpper(prev) && unicode.IsUpper(cur) &&
			i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
			boundary = true
		}
		if boundary {
			tokens = append(tokens, string(runes[start:i]))
			start = i
		}
	}
	tokens = append(tokens, string(runes[start:]))
	return tokens
}

func joinTokens(tokens []string) string {
	return strings.Join(tokens, " ")
}
