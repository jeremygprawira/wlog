package redact

// Redactor masks sensitive keys and values in an event. It is immutable after New
// returns; to change what it masks, build a new one (see With, added in a later task).
type Redactor struct {
	keys map[string]bool // token-joined denylist entries, e.g. "auth" or "api key"
}

// Option configures a Redactor built by New.
type Option func(*config)

type config struct {
	keys []string
}

// New compiles opts into an immutable *Redactor. With no options, it uses defaultKeys.
func New(opts ...Option) (*Redactor, error) {
	c := &config{keys: append([]string(nil), defaultKeys...)}
	for _, opt := range opts {
		opt(c)
	}
	keys := make(map[string]bool, len(c.keys))
	for _, k := range c.keys {
		keys[joinTokens(tokenize(k))] = true
	}
	return &Redactor{keys: keys}, nil
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
	r.applyMap(event)
}

func (r *Redactor) applyMap(m map[string]any) {
	for k, v := range m {
		if r.matchesKey(k) {
			m[k] = "[REDACTED]"
			continue
		}
		m[k] = r.applyValue(v)
	}
}

func (r *Redactor) applyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		r.applyMap(x)
		return x
	case []any:
		for i, item := range x {
			x[i] = r.applyValue(item)
		}
		return x
	default:
		return v
	}
}

// matchesKey reports whether any contiguous run of key's tokens equals a denylist
// entry, e.g. "stripe_api_key" contains the run ["api","key"] and matches "api_key".
func (r *Redactor) matchesKey(key string) bool {
	tokens := tokenize(key)
	for i := 0; i < len(tokens); i++ {
		for j := i + 1; j <= len(tokens); j++ {
			if r.keys[joinTokens(tokens[i:j])] {
				return true
			}
		}
	}
	return false
}
