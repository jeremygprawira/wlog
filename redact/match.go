package redact

import (
	"fmt"
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

// reservedShape names the reserved fields of the event shape, one segment at a
// time. A dotted entry whose first segment is a reserved group is checked against
// this list, because a path that can never match a reserved field is a typo: a
// caller writes http.request.header.authorization while the shape names
// http.request_headers, and the mistake would otherwise be silent.
var reservedShape = [][]string{
	{"http", "request_headers"},
	{"http", "request_cookies"},
	{"http", "request_query"},
	{"http", "request_body"},
	{"http", "response_headers"},
	{"http", "response_body"},
	{"http", "method"},
	{"http", "path"},
	{"http", "route"},
	{"http", "status"},
	{"http", "bytes_in"},
	{"http", "bytes_out"},
	{"http", "client_ip"},
	{"http", "user_agent"},
	{"http", "duration_ms"},
	{"trace", "request_id"},
	{"trace", "trace_id"},
	{"trace", "span_id"},
	{"trace", "parent_operation"},
	{"audit", "action"},
	{"audit", "actor"},
	{"audit", "target"},
	{"audit", "outcome"},
	{"service", "name"},
	{"service", "version"},
	{"service", "env"},
	{"faas", "name"},
	{"faas", "request_id"},
	{"llm", "request_model"},
	{"llm", "calls"},
}

// validatePathEntry reports an entry that can never match a reserved field.
//
// An entry whose first segment is not a reserved group is a path into the caller's
// own event, so it is always valid. A reserved entry must follow the shape: each
// segment must be the start of a reserved segment at that position, which accepts
// http.request_headers.authorization and rejects http.request.header.
func validatePathEntry(entry string) error {
	segments := strings.Split(entry, ".")
	group := strings.ToLower(segments[0])
	reserved := false
	for _, path := range reservedShape {
		if path[0] == group {
			reserved = true
			break
		}
	}
	if !reserved {
		return nil
	}
	for i, segment := range segments {
		if i >= 2 {
			// Past the known shape: a header or a field of the caller's own
			// making, which the redactor cannot check.
			return nil
		}
		found := false
		for _, path := range reservedShape {
			if path[0] != group || i >= len(path) {
				continue
			}
			// The second segment must be a reserved field, not a prefix of one,
			// because the shape names the field in full.
			match := path[i] == strings.ToLower(segment)
			if i == 0 {
				match = path[i] == group
			}
			if match {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("path %q can never match a reserved field", entry)
		}
	}
	return nil
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
