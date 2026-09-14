package redact

import "strings"

// Redactor masks sensitive keys and values in an event. It is immutable after New
// returns; to change what it masks, build a new one (see With, added in a later task).
type Redactor struct {
	leafTokens map[string]bool // no-dot, no-star entries: joined tokens, e.g. "auth"
	leafGlobs  []string        // no-dot, has-star entries: lowercased glob, e.g. "*_pin"
	paths      [][]segMatcher  // dotted entries, one segMatcher per "." segment
}

// Option configures a Redactor built by New.
type Option func(*config)

type config struct {
	keys []string
}

// ReplaceKeys starts the key denylist from exactly this list, with no defaults.
func ReplaceKeys(keys ...string) Option {
	return func(c *config) { c.keys = append([]string(nil), keys...) }
}

// New compiles opts into an immutable *Redactor. With no options, it uses defaultKeys.
func New(opts ...Option) (*Redactor, error) {
	c := &config{keys: append([]string(nil), defaultKeys...)}
	for _, opt := range opts {
		opt(c)
	}

	r := &Redactor{leafTokens: map[string]bool{}}
	for _, k := range c.keys {
		switch {
		case strings.Contains(k, "."):
			var segs []segMatcher
			for _, seg := range strings.Split(k, ".") {
				segs = append(segs, newSegMatcher(seg))
			}
			r.paths = append(r.paths, segs)
		case strings.Contains(k, "*"):
			r.leafGlobs = append(r.leafGlobs, strings.ToLower(k))
		default:
			r.leafTokens[joinTokens(tokenize(k))] = true
		}
	}
	return r, nil
}

// MustNew is New, panicking on error. Use it in package-level vars and tests.
func MustNew(opts ...Option) *Redactor {
	r, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return r
}

// Apply walks event and replaces the value of any key matching the denylist with
// "[REDACTED]". It mutates event in place: core hands Apply a private snapshot that
// nothing else holds a reference to.
func (r *Redactor) Apply(event map[string]any) {
	r.applyMap(event, nil)
}

func (r *Redactor) applyMap(m map[string]any, path []string) {
	for k, v := range m {
		fullPath := append(append([]string(nil), path...), k)
		if r.matchesKey(k) || r.matchesLeafGlob(k) || r.matchesPath(fullPath) {
			m[k] = "[REDACTED]"
			continue
		}
		m[k] = r.applyValue(v, fullPath)
	}
}

func (r *Redactor) applyValue(v any, path []string) any {
	switch x := v.(type) {
	case map[string]any:
		r.applyMap(x, path)
		return x
	case []any:
		for i, item := range x {
			// Array elements inherit the parent field's path (no index segment),
			// so "items.card_number" matches every element's card_number.
			x[i] = r.applyValue(item, path)
		}
		return x
	default:
		return v
	}
}

// matchesKey reports whether any contiguous run of key's tokens equals a leaf denylist
// entry, e.g. "stripe_api_key" contains the run ["api","key"] and matches "api_key".
// Unanchored: matches at any depth.
func (r *Redactor) matchesKey(key string) bool {
	tokens := tokenize(key)
	for i := 0; i < len(tokens); i++ {
		for j := i + 1; j <= len(tokens); j++ {
			if r.leafTokens[joinTokens(tokens[i:j])] {
				return true
			}
		}
	}
	return false
}

// matchesLeafGlob reports whether key (lowercased) matches any unanchored glob entry.
func (r *Redactor) matchesLeafGlob(key string) bool {
	lower := strings.ToLower(key)
	for _, g := range r.leafGlobs {
		if ok := globMatch(g, lower); ok {
			return true
		}
	}
	return false
}

// matchesPath reports whether fullPath (from event root to this field) matches any
// dotted entry: same segment count, each segment matching the entry's segMatcher.
func (r *Redactor) matchesPath(fullPath []string) bool {
	for _, segs := range r.paths {
		if len(segs) != len(fullPath) {
			continue
		}
		matched := true
		for i, sm := range segs {
			if !sm.match(fullPath[i]) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
