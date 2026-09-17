package sample

import (
	"fmt"
	"path"
	"strings"
)

// doubleStar is the segment that matches any run of segments, including none.
const doubleStar = "**"

// matchGlob reports whether p matches pattern.
//
// A ** segment matches any number of segments, including none, so "/api/**" covers "/api",
// "/api/payments", and "/api/payments/123". Every other segment matches with path.Match
// semantics, so a * stays inside one segment.
func matchGlob(pattern, p string) bool {
	patternSegments := strings.Split(strings.Trim(pattern, "/"), "/")
	pathSegments := strings.Split(strings.Trim(p, "/"), "/")
	return matchSegments(patternSegments, pathSegments)
}

// matchSegments matches the pattern segments against the path segments, moving on when a **
// segment consumes none, one, or many of them.
func matchSegments(pattern, pathSegments []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == doubleStar {
			// ** may end the pattern, or it may consume any run of path segments: try every
			// split from the shortest to the longest.
			for i := 0; i <= len(pathSegments); i++ {
				if matchSegments(pattern[1:], pathSegments[i:]) {
					return true
				}
			}
			return false
		}
		if len(pathSegments) == 0 {
			return false
		}
		matched, err := path.Match(pattern[0], pathSegments[0])
		if err != nil || !matched {
			return false
		}
		pattern, pathSegments = pattern[1:], pathSegments[1:]
	}
	return len(pathSegments) == 0
}

// validateGlob reports whether a glob can match anything.
//
// A ** must stand alone in its segment: a pattern that mixes ** with other characters, such as
// "x**", is a mistake rather than a rule, and a pattern path.Match cannot compile is refused
// too. A sampler that kept every event because its glob was a typo would be a silent hole in a
// team's data.
func validateGlob(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("sample: glob is empty")
	}
	for _, segment := range strings.Split(strings.Trim(pattern, "/"), "/") {
		if strings.Contains(segment, doubleStar) && segment != doubleStar {
			return fmt.Errorf("sample: glob %q mixes ** with other characters in %q", pattern, segment)
		}
		if segment == doubleStar {
			continue
		}
		if _, err := path.Match(segment, "probe"); err != nil {
			return fmt.Errorf("sample: glob %q is invalid: %w", pattern, err)
		}
	}
	return nil
}
