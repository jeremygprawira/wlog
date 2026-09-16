package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Redactor masks sensitive keys and values in an event. It is immutable after New
// returns; to change what it masks, derive a new one with With.
type Redactor struct {
	disabled        bool            // Disabled(): Apply is a no-op, nothing else below is set
	raw             []string        // the effective raw key entries, post add/remove
	leafTokens      map[string]bool // no-dot, no-star entries: joined tokens, e.g. "auth"
	leafGlobs       []string        // no-dot, has-star entries: lowercased glob, e.g. "*_pin"
	paths           [][]segMatcher  // dotted entries, one segMatcher per "." segment
	patterns        []builtinPattern
	maskClientIP    bool
	transforms      []func(map[string]any)
	replacement     string
	replaceFunc     func(string) string
	maxDepth        int
	maxStringScan   int
	tokenCache      sync.Map     // key string -> []string; a real event reuses the same
	cacheEntries    atomic.Int64 // field names on every call, so a fresh tokenize is waste
	maxLeafTokens   int          // longest denylist entry in tokens, which bounds a run
	customPatterns  []Pattern    // the patterns a caller added, for a later With
	builtins        []string     // the active built-in pattern names, for a later With
	caseInsensitive bool         // whether key matching folds case
}

// cachedTokenize is tokenize(key), memoized per Redactor. Safe for concurrent use
// (sync.Map): tokenize is a pure function, so a duplicate compute on a cache race
// just replaces the entry with an equal value.
//
// The cache holds at most cacheLimit keys. Past that a key is tokenized without
// being stored, so a caller who sends a new field name on every event cannot grow
// the redactor without bound.
func (r *Redactor) cachedTokenize(key string) []string {
	key = cutKey(key)
	if v, ok := r.tokenCache.Load(key); ok {
		return v.([]string)
	}
	tokens := tokenize(key)
	if r.cacheEntries.Add(1) > cacheLimit {
		r.cacheEntries.Add(-1)
		return tokens
	}
	if _, loaded := r.tokenCache.LoadOrStore(key, tokens); loaded {
		r.cacheEntries.Add(-1)
	}
	return tokens
}

// cutKey shortens a key to the length that the denylist can answer for. Every
// entry is far shorter than the limit, so the tail of a hostile key cannot change
// a match, and the work stays linear in the length of a key that matters.
func cutKey(key string) string {
	if len(key) <= maxKeyBytes {
		return key
	}
	return key[:maxKeyBytes]
}

const (
	defaultMaxDepth      = 16
	defaultMaxStringScan = 64 * 1024
	// maxKeyBytes is the longest field name that can match a denylist entry.
	maxKeyBytes = 256
	// cacheLimit bounds the token cache. The bound keeps a stream of new field
	// names from growing the process while it still holds every name a real
	// service uses.
	cacheLimit = 4096
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
	replaceFunc       func(string) string
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

// ReplaceFunc sets a function that computes the mask from the matched value, so a
// caller can keep a last-4 tail, a domain, or a stable hash. It replaces the fixed
// replacement text for a key, path, or glob match. A panic in fn falls back to the
// fixed replacement string, so a bad mask function can never leak the raw value.
func ReplaceFunc(fn func(match string) string) Option {
	return func(c *config) { c.replaceFunc = fn }
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
	if r.disabled {
		// Disabled() means "no redaction", and a derived redactor would silently
		// turn masking back on. Say so instead.
		return nil, fmt.Errorf("redact: With on a disabled redactor")
	}
	c := r.config()
	return build(c, opts)
}

// config returns the resolved configuration of a redactor, so With can start from
// every setting rather than from the defaults: the denylist, the pattern toggles,
// the custom patterns, the transforms, the ReplaceFunc, the masks, and the limits.
//
// A redactor keeps the raw denylist, so a key that RemoveKeys took out stays out
// and a key it kept stays in. The pattern toggles are replayed from the compiled
// set: a built-in name is enabled when the redactor holds it, and every custom
// pattern is kept whole.
func (r *Redactor) config() *config {
	c := newConfig()
	c.keys = append([]string(nil), r.raw...)
	c.maskClientIP = r.maskClientIP
	c.replacement = r.replacement
	c.maxDepth = r.maxDepth
	c.maxStringScan = r.maxStringScan
	c.transforms = append(c.transforms, r.transforms...)
	c.replaceFunc = r.replaceFunc
	c.customPatterns = append([]Pattern(nil), r.customPatterns...)
	// A built-in pattern that the receiver holds is enabled here, and one it lost
	// stays lost, so With never turns a removed pattern back on.
	c.noBuiltinPatterns = true
	c.enabledPatterns = append([]string(nil), r.builtins...)
	c.removedPatterns = nil
	return c
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
		replaceFunc:   c.replaceFunc,
		maxDepth:      c.maxDepth,
		maxStringScan: c.maxStringScan,
		// With starts from these, so a derived redactor keeps the custom patterns,
		// the case setting, and the exact set of built-in patterns in force.
		customPatterns: append([]Pattern(nil), c.customPatterns...),
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
			tokens := tokenize(cutKey(k))
			r.leafTokens[joinTokens(tokens)] = true
			// A key entry also matches its tokens joined with no separator, so
			// "api_key" matches a field named "apikey" and "session_id" matches
			// "sessionid". Naming one thing two ways is common, and a denylist
			// that misses the second name is a denylist with a hole.
			if joined := strings.Join(tokens, ""); joined != joinTokens(tokens) {
				r.leafTokens[joined] = true
			}
			if len(tokens) > r.maxLeafTokens {
				r.maxLeafTokens = len(tokens)
			}
		}
	}
	for _, p := range r.patterns {
		if !p.custom {
			r.builtins = append(r.builtins, p.name)
		}
	}
	if r.maxLeafTokens == 0 {
		r.maxLeafTokens = 1
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
	defer func() { _ = recover() }()
	t(event)
}

// maskValue returns the mask for one denied value. With no ReplaceFunc it is the fixed
// replacement. With one, a string value goes through the function, and a panic falls
// back to the fixed text.
func (r *Redactor) maskValue(v any) any {
	if r.replaceFunc == nil {
		return r.replacement
	}
	raw, ok := v.(string)
	if !ok {
		return r.replacement
	}
	return r.safeReplace(raw)
}

// safeReplace runs the replacement function, recovering a panic into the fixed text.
func (r *Redactor) safeReplace(match string) (out string) {
	defer func() {
		if recover() != nil {
			out = r.replacement
		}
	}()
	return r.replaceFunc(match)
}

// applyMap walks m, mutating path in place (push the key, recurse, pop) rather than
// copying it per field. Safe because path never escapes this call tree: nothing keeps
// a reference to it past the synchronous matchesPath/applyValue calls below.
func (r *Redactor) applyMap(m map[string]any, path []string, depth int) {
	for k, v := range m {
		path = append(path, k)
		if r.matchesKey(k) || r.matchesLeafGlob(k) || r.matchesPath(path) {
			m[k] = r.maskValue(v)
		} else {
			m[k] = r.applyValue(v, path, depth)
		}
		path = path[:len(path)-1]
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
	tokens := r.cachedTokenize(key)
	// A run is never longer than the longest denylist entry, so a long key costs
	// a bounded number of lookups rather than a quadratic scan.
	for i := range tokens {
		limit := min(i+r.maxLeafTokens, len(tokens))
		for j := i + 1; j <= limit; j++ {
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

// Denies reports whether a top-level key name would be masked by the key denylist. It
// checks the key tokens and the leaf globs, not the value patterns, because a value
// pattern only applies after the event holds a value. A linter (cli-map) uses this to
// flag a literal key that the redactor would already deny.
func (r *Redactor) Denies(key string) bool {
	return r.matchesKey(key) || r.matchesLeafGlob(key)
}

// Fingerprint is a short, stable hash of the effective config: the same set of keys
// hashes the same regardless of the order options were given, and changes after any
// add or remove.
func (r *Redactor) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join(r.Keys(), "\n")))
	return hex.EncodeToString(sum[:8])
}
