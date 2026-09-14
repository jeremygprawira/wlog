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
	disabled      bool            // Disabled(): Apply is a no-op, nothing else below is set
	raw           []string        // the effective raw key entries, post add/remove
	leafTokens    map[string]bool // no-dot, no-star entries: joined tokens, e.g. "auth"
	leafGlobs     []string        // no-dot, has-star entries: lowercased glob, e.g. "*_pin"
	paths         [][]segMatcher  // dotted entries, one segMatcher per "." segment
	patterns      []builtinPattern
	maskClientIP  bool
	transforms    []func(map[string]any)
	replacement   string
	maxDepth      int
	maxStringScan int
}

const (
	defaultMaxDepth      = 16
	defaultMaxStringScan = 64 * 1024
)

// Option configures a Redactor built by New or With.
type Option func(*config)

type config struct {
	keys              []string
	removed           []string
	enabledPatterns   []string
	maskClientIP      bool
	customPatterns    []Pattern
	removedPatterns   []string
	noBuiltinPatterns bool
	transforms        []func(map[string]any)
	replacement       string
	maxDepth          int
	maxStringScan     int
}

// Transform registers a function that runs on the whole event before the key, glob,
// path, and pattern rules do. Use it for logic those rules can't express, e.g.
// dropping a field only for one tenant. A panic in fn is recovered: that transform is
// skipped and the rest of Apply still runs.
func Transform(fn func(map[string]any)) Option {
	return func(c *config) { c.transforms = append(c.transforms, fn) }
}

// Replacement sets the text a matched key, path, or glob is replaced with. Default
// "[REDACTED]".
func Replacement(s string) Option {
	return func(c *config) { c.replacement = s }
}

// MaxDepth caps how many levels of nested maps/arrays Apply walks into. A subtree
// deeper than this is replaced with "[REDACTED:DEPTH]" instead of being masked
// field-by-field. Default 16.
func MaxDepth(n int) Option {
	return func(c *config) { c.maxDepth = n }
}

// MaxStringScan caps how many bytes of one string value the built-in and custom value
// patterns scan. A longer string is replaced with "[REDACTED:TOO_LARGE]" instead
// (its key may still mask it first, same as any other field). Default 64KB.
func MaxStringScan(n int) Option {
	return func(c *config) { c.maxStringScan = n }
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
	c := newConfig()
	c.keys = append([]string(nil), defaultKeys...)
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

// Default is the Redactor New() with no options builds: every default on. Use it as an
// explicit "no custom redactor configured" fallback.
func Default() *Redactor {
	return MustNew()
}

// Disabled turns redaction off entirely: Apply becomes a no-op. Prefer this over
// omitting a redactor, so "no redaction" is a deliberate, greppable choice.
func Disabled() *Redactor {
	return &Redactor{disabled: true}
}

// With derives a new Redactor from r's effective config plus opts. r is unchanged.
func (r *Redactor) With(opts ...Option) (*Redactor, error) {
	c := newConfig()
	c.keys = append([]string(nil), r.raw...)
	c.maskClientIP, c.replacement, c.maxDepth, c.maxStringScan = r.maskClientIP, r.replacement, r.maxDepth, r.maxStringScan
	return build(c, opts)
}

func newConfig() *config {
	return &config{replacement: "[REDACTED]", maxDepth: defaultMaxDepth, maxStringScan: defaultMaxStringScan}
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

	patterns, err := buildPatterns(c)
	if err != nil {
		return nil, err
	}
	r := &Redactor{
		raw:           append([]string(nil), final...),
		leafTokens:    map[string]bool{},
		maskClientIP:  c.maskClientIP,
		patterns:      patterns,
		transforms:    c.transforms,
		replacement:   c.replacement,
		maxDepth:      c.maxDepth,
		maxStringScan: c.maxStringScan,
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

// Apply walks event and replaces the value of any key matching the denylist with the
// configured Replacement. It mutates event in place: core hands Apply a private
// snapshot that nothing else holds a reference to.
func (r *Redactor) Apply(event map[string]any) {
	if r.disabled {
		return
	}
	r.runTransforms(event)
	r.applyMap(event, nil, 0)
}

// runTransforms runs every registered Transform. A panic in one is recovered and that
// transform is skipped; the rest still run, and redaction still proceeds afterward.
func (r *Redactor) runTransforms(event map[string]any) {
	for _, t := range r.transforms {
		runTransformSafe(t, event)
	}
}

func runTransformSafe(t func(map[string]any), event map[string]any) {
	defer func() { recover() }()
	t(event)
}

func (r *Redactor) applyMap(m map[string]any, path []string, depth int) {
	for k, v := range m {
		fullPath := append(append([]string(nil), path...), k)
		if r.matchesKey(k) || r.matchesLeafGlob(k) || r.matchesPath(fullPath) {
			m[k] = r.replacement
			continue
		}
		m[k] = r.applyValue(v, fullPath, depth)
	}
}

func (r *Redactor) applyValue(v any, path []string, depth int) any {
	switch x := v.(type) {
	case map[string]any:
		if depth+1 > r.maxDepth {
			return "[REDACTED:DEPTH]"
		}
		r.applyMap(x, path, depth+1)
		return x
	case []any:
		if depth+1 > r.maxDepth {
			return "[REDACTED:DEPTH]"
		}
		for i, item := range x {
			// Array elements inherit the parent field's path (no index segment),
			// so "items.card_number" matches every element's card_number.
			x[i] = r.applyValue(item, path, depth+1)
		}
		return x
	case string:
		if len(x) > r.maxStringScan {
			return "[REDACTED:TOO_LARGE]"
		}
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
