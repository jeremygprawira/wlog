package redact

import (
	"path"
	"strings"
)

// segMatcher matches one path segment of a dotted denylist entry, e.g. "headers" or
// "*" in "http.request.headers.*".
type segMatcher struct {
	isGlob bool
	glob   string   // lowercased pattern, used when isGlob
	tokens []string // used when !isGlob: must appear contiguously in the segment's tokens
}

func newSegMatcher(seg string) (segMatcher, error) {
	if strings.Contains(seg, "*") {
		if err := validateGlob(seg); err != nil {
			return segMatcher{}, err
		}
		return segMatcher{isGlob: true, glob: strings.ToLower(seg)}, nil
	}
	return segMatcher{tokens: tokenize(seg)}, nil
}

// validateGlob reports an error if pattern is not a valid "*"-only glob, e.g. an
// unterminated "[" bracket class. New/With surface this instead of panicking or
// silently treating the entry as never-matching.
func validateGlob(pattern string) error {
	_, err := path.Match(strings.ToLower(pattern), "")
	return err
}

func (m segMatcher) match(actualSeg string) bool {
	if m.isGlob {
		return globMatch(m.glob, strings.ToLower(actualSeg))
	}
	return containsRun(tokenize(actualSeg), m.tokens)
}

// globMatch matches pattern (already lowercased) against s (already lowercased),
// supporting only the "*" wildcard. An invalid pattern never matches.
func globMatch(pattern, s string) bool {
	ok, err := path.Match(pattern, s)
	return err == nil && ok
}

// containsRun reports whether needle appears as a contiguous run inside hay.
func containsRun(hay, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(hay) {
		return false
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j, tok := range needle {
			if hay[i+j] != tok {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
