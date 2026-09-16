package redact

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern is a user-defined value pattern, added with AddPatterns. It works exactly
// like a built-in pattern: Regex finds candidates in a string value, and either
// Replace or Replacement decides what replaces each match.
type Pattern struct {
	Name        string
	Regex       string
	Replacement string // used when Replace is nil; default "[REDACTED]"
	Replace     func(Match) string
}

// Match describes one pattern hit, passed to a Pattern's Replace func.
type Match struct {
	Path   string // dotted path from the event root, e.g. "http.request.headers.authorization"
	Key    string // the leaf field name
	Value  string // the full matched text
	Groups []string
}

func (p Pattern) masker() func(Match) string {
	return func(m Match) string {
		if p.Replace != nil {
			return p.Replace(m)
		}
		if p.Replacement != "" {
			return p.Replacement
		}
		return "[REDACTED]"
	}
}

// AddPatterns adds custom value patterns alongside the built-ins.
func AddPatterns(patterns ...Pattern) Option {
	return func(c *config) { c.customPatterns = append(c.customPatterns, patterns...) }
}

// RemovePatterns turns off named patterns, built-in or custom. New/With return an
// error if a name is not currently active.
func RemovePatterns(names ...string) Option {
	return func(c *config) { c.removedPatterns = append(c.removedPatterns, names...) }
}

// NoBuiltinPatterns turns off every built-in value pattern; only AddPatterns entries
// (and anything EnablePatterns would have turned on) run.
func NoBuiltinPatterns() Option {
	return func(c *config) { c.noBuiltinPatterns = true }
}

// builtinNames returns the name of every built-in pattern.
func builtinNames() []string {
	names := make([]string, 0, len(allBuiltinPatterns))
	for _, p := range allBuiltinPatterns {
		names = append(names, p.name)
	}
	return names
}

// buildPatterns resolves defaults, EnablePatterns, AddPatterns, RemovePatterns and
// NoBuiltinPatterns into the final ordered, name-unique pattern list.
func buildPatterns(c *config) ([]builtinPattern, error) {
	// A pattern name that no built-in carries is a typo, and a typo must not read
	// as "the pattern is off".
	for _, name := range append(append([]string(nil), c.enabledPatterns...), c.removedPatterns...) {
		if indexFold(builtinNames(), name) < 0 {
			return nil, fmt.Errorf("redact: unknown pattern name %q", name)
		}
	}

	var patterns []builtinPattern
	for _, p := range allBuiltinPatterns {
		switch {
		case !c.noBuiltinPatterns && (p.enabledByDefault || indexFold(c.enabledPatterns, p.name) >= 0):
			patterns = append(patterns, p)
		case c.noBuiltinPatterns && indexFold(c.enabledPatterns, p.name) >= 0:
			// With replays the exact set of built-ins the receiver holds, so a
			// pattern that RemovePatterns took out never comes back.
			patterns = append(patterns, p)
		}
	}

	for _, up := range c.customPatterns {
		up := up
		if patternIndex(patterns, up.Name) >= 0 {
			return nil, fmt.Errorf("redact: duplicate pattern name %q", up.Name)
		}
		re, err := regexp.Compile(up.Regex)
		if err != nil {
			return nil, fmt.Errorf("redact: invalid pattern %q: %w", up.Name, err)
		}
		// A pattern that matches the empty string would insert a mask between
		// every pair of characters, so it is a mistake rather than a rule.
		if re.MatchString("") {
			return nil, fmt.Errorf("redact: pattern %q matches the empty string", up.Name)
		}
		patterns = append(patterns, builtinPattern{name: up.Name, re: re, masker: up.masker(), custom: true})
	}

	for _, rem := range c.removedPatterns {
		idx := patternIndex(patterns, rem)
		if idx < 0 {
			return nil, fmt.Errorf("redact: RemovePatterns: %q is not active", rem)
		}
		patterns = append(patterns[:idx], patterns[idx+1:]...)
	}
	return patterns, nil
}

func patternIndex(patterns []builtinPattern, name string) int {
	for i, p := range patterns {
		if strings.EqualFold(p.name, name) {
			return i
		}
	}
	return -1
}
