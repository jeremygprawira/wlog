package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Redactor masks sensitive keys and values in an event. It is immutable after New
// returns; to change what it masks, derive a new one with With.
type Redactor struct {
	raw          []string        // the effective raw key entries, post add/remove
	leafTokens   map[string]bool // no-dot, no-star entries: joined tokens, e.g. "auth"
	leafGlobs    []string        // no-dot, has-star entries: lowercased glob, e.g. "*_pin"
	paths        [][]segMatcher  // dotted entries, one segMatcher per "." segment
	patterns     []builtinPattern
	maskClientIP bool
}

// Option configures a Redactor built by New or With.
type Option func(*config)

type config struct {
	keys            []string
	removed         []string
	enabledPatterns []string
	maskClientIP    bool
}

// EnablePatterns turns on a built-in pattern that is off by default (currently only
// "nik", which has a higher false-positive rate than the others).
func EnablePatterns(names ...string) Option {
	return func(c *config) { c.enabledPatterns = append(c.enabledPatterns, names...) }
}

// MaskClientIP also masks the reserved http.client_ip field with the ipv4 pattern. By
// default that field is exempt, since it is IP metadata core adds on purpose, not a
// value that happened to contain an address.
func MaskClientIP() Option {
	return func(c *config) { c.maskClientIP = true }
}

// AddKeys extends the key denylist. See match.go for how an entry matches: no dot and
// no star matches a whole word at any depth, a star globs within one segment, a dot
// anchors the entry to that exact path from the event root.
func AddKeys(keys ...string) Option {
	return func(c *config) { c.keys = append(c.keys, keys...) }
}

// RemoveKeys removes entries from the current denylist (the defaults, or whatever an
// earlier AddKeys/ReplaceKeys built). New/With return an error if an entry is not
// present, so a typo in RemoveKeys never silently keeps a key masked.
func RemoveKeys(keys ...string) Option {
	return func(c *config) { c.removed = append(c.removed, keys...) }
}

// ReplaceKeys starts the key denylist from exactly this list, with no defaults and no
// earlier AddKeys/RemoveKeys in this Option chain.
func ReplaceKeys(keys ...string) Option {
	return func(c *config) { c.keys, c.removed = append([]string(nil), keys...), nil }
}

// New compiles opts into an immutable *Redactor. With no options, it uses defaultKeys.
func New(opts ...Option) (*Redactor, error) {
	c := &config{keys: append([]string(nil), defaultKeys...)}
	return build(c, opts)
}

// MustNew is New, panicking on error. Use it in package-level vars and tests.
func MustNew(opts ...Option) *Redactor {
	r, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return r
}

// With derives a new Redactor from r's effective key list plus opts. r is unchanged.
func (r *Redactor) With(opts ...Option) (*Redactor, error) {
	c := &config{keys: append([]string(nil), r.raw...)}
	return build(c, opts)
}

// build applies opts to c, resolves removals, validates every glob, and compiles the
// result. It never panics: every failure is returned as an error.
func build(c *config, opts []Option) (*Redactor, error) {
	for _, opt := range opts {
		opt(c)
	}

	final := c.keys
	for _, rem := range c.removed {
		idx := indexFold(final, rem)
		if idx < 0 {
			return nil, fmt.Errorf("redact: RemoveKeys: %q is not in the current denylist", rem)
		}
		final = append(final[:idx], final[idx+1:]...)
	}

	r := &Redactor{
		raw:          append([]string(nil), final...),
		leafTokens:   map[string]bool{},
		maskClientIP: c.maskClientIP,
	}
	for _, p := range allBuiltinPatterns {
		if p.enabledByDefault || indexFold(c.enabledPatterns, p.name) >= 0 {
			r.patterns = append(r.patterns, p)
		}
	}
	for _, k := range r.raw {
		switch {
		case strings.Contains(k, "."):
			var segs []segMatcher
			for _, seg := range strings.Split(k, ".") {
				sm, err := newSegMatcher(seg)
				if err != nil {
					return nil, fmt.Errorf("redact: invalid path entry %q: %w", k, err)
				}
				segs = append(segs, sm)
			}
			r.paths = append(r.paths, segs)
		case strings.Contains(k, "*"):
			if err := validateGlob(k); err != nil {
				return nil, fmt.Errorf("redact: invalid glob entry %q: %w", k, err)
			}
			r.leafGlobs = append(r.leafGlobs, strings.ToLower(k))
		default:
			r.leafTokens[joinTokens(tokenize(k))] = true
		}
	}
	return r, nil
}

func indexFold(list []string, want string) int {
	for i, s := range list {
		if strings.EqualFold(s, want) {
			return i
		}
	}
	return -1
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
	case string:
		return r.applyPatterns(x, path)
	default:
		return v
	}
}

// matchesKey reports whether any contiguous run of key's tokens equals a leaf denylist
// entry, e.g. "stripe_api_key" contains the run ["api","key"] and matches "api_key".
// Unanchored: matches at any depth.
func (r *Redactor) matchesKey(key string) bool {
	tokens := tokenize(key)
	for i := range tokens {
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
		if globMatch(g, lower) {
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

// Keys returns the effective key denylist, sorted.
func (r *Redactor) Keys() []string {
	out := append([]string(nil), r.raw...)
	sort.Strings(out)
	return out
}

// Fingerprint is a short, stable hash of the effective config: the same set of keys
// hashes the same regardless of the order options were given, and changes after any
// add or remove.
func (r *Redactor) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join(r.Keys(), "\n")))
	return hex.EncodeToString(sum[:8])
}
