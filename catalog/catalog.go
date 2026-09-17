package catalog

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrUnknownCode is wrapped by Err for a code the registry does not hold, so a caller
// can test for it with errors.Is.
var ErrUnknownCode = errors.New("catalog: unknown code")

// Registry holds one domain's entries under one prefix.
type Registry struct {
	prefix string
	// domain is the lower-case prefix a record reports as the code's domain, so an event
	// reads "billing" while the wire code reads "BILLING_DECLINED".
	domain  string
	entries map[string]Entry // keyed by full code
	short   map[string]string
	// allowShort lets the extractor resolve a short code too. Off by default, so the
	// default extractor's plain "INTERNAL" cannot match an entry named "internal".
	allowShort bool
}

// New builds a registry from a prefix and its entries. The full code is the upper-case
// prefix, an underscore, and the upper-case short code. New panics on an empty prefix,
// an empty code, or a duplicate code, because each is an author mistake that must fail
// at startup.
func New(prefix string, entries ...Entry) *Registry {
	p := strings.ToUpper(strings.TrimSpace(prefix))
	if p == "" {
		panic("catalog: prefix is empty")
	}
	r := &Registry{prefix: p, domain: strings.ToLower(strings.TrimSpace(prefix)), entries: map[string]Entry{}, short: map[string]string{}}
	for _, entry := range entries {
		full, short := splitCode(p, entry.Code)
		if short == "" {
			panic("catalog: entry code is empty")
		}
		if _, duplicate := r.entries[full]; duplicate {
			panic("catalog: duplicate code " + full)
		}
		copied := entry
		copied.domain = r.domain
		copied.full = full
		copied.Code = short
		if entry.Audit != nil {
			audit := *entry.Audit
			copied.Audit = &audit
		}
		r.entries[full] = copied
		r.short[short] = full
	}
	return r
}

// Prefix returns the upper-case prefix.
func (r *Registry) Prefix() string { return r.prefix }

// Domain returns the lower-case prefix a record reports as the code's domain, as in
// "billing" for the code BILLING_DECLINED.
func (r *Registry) Domain() string { return r.domain }

// Codes returns every full code, sorted.
func (r *Registry) Codes() []string {
	codes := make([]string, 0, len(r.entries))
	for code := range r.entries {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// AllowShortCodes returns a copy of the registry whose extractor also resolves a short
// code. The original registry is unchanged.
//
// Matching a full code is the default, because a short code is easy to hit by accident: the
// default extractor's own "INTERNAL" would otherwise pick up an entry named "internal" and
// attach its status and guidance to every plain error.
func (r *Registry) AllowShortCodes() *Registry {
	derived := *r
	derived.allowShort = true
	return &derived
}

// getFull resolves a full, prefixed code, which is what the extractor matches.
func (r *Registry) getFull(code string) (Entry, bool) {
	entry, ok := r.entries[strings.ToUpper(strings.TrimSpace(code))]
	return entry, ok
}

// Get resolves a full or a short code to its entry. It returns a deep copy, so a caller
// cannot change what the registry holds.
func (r *Registry) Get(code string) (Entry, bool) {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if entry, ok := r.entries[upper]; ok {
		return entry.clone(), true
	}
	if full, ok := r.short[upper]; ok {
		return r.entries[full].clone(), true
	}
	return Entry{}, false
}

// Err builds an error for one code. params are key, value pairs that fill the message
// template, plus an optional "cause" pair whose error value becomes the wrapped cause.
// An odd trailing error value is also treated as the cause.
func (r *Registry) Err(code string, params ...any) error {
	entry, ok := r.Get(code)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCode, code)
	}
	message, cause := render(entry.Message, params)
	if message == "" {
		message = fullCode(r.prefix, entry.Code)
	}
	return &codedError{
		domain:  r.domain,
		full:    entry.full,
		message: message,
		entry:   entry.clone(),
		cause:   cause,
	}
}

// fullCode joins a prefix and a short code into the wire code.
func fullCode(prefix, short string) string {
	return strings.ToUpper(prefix) + "_" + strings.ToUpper(short)
}

// splitCode returns the full and the short code of one declared entry.
//
// A code already qualified with the registry's own prefix keeps it, so an error library
// that hands over a domain-qualified code (a herr class code, say) matches the same entry
// its short spelling does, and the same registry serves both libraries.
func splitCode(prefix, code string) (full, short string) {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if qualified, ok := strings.CutPrefix(upper, prefix+"_"); ok {
		return upper, qualified
	}
	return fullCode(prefix, upper), upper
}

// render fills {name} placeholders from key, value pairs. A placeholder with no pair stays
// as written, so a missing value never panics. It also returns the cause.
//
// It renders in one pass: a value that itself holds a placeholder is written as it stands
// and never filled again, so a parameter can never rewrite another parameter's text.
func render(template string, params []any) (string, error) {
	var cause error
	pairs := make([]string, 0, len(params))
	for i := 0; i+1 < len(params); i += 2 {
		key, ok := params[i].(string)
		if !ok {
			continue
		}
		value := params[i+1]
		if key == "cause" {
			if err, ok := value.(error); ok {
				cause = err
			}
		}
		pairs = append(pairs, "{"+key+"}", fmt.Sprint(value))
	}
	if len(params)%2 == 1 {
		if err, ok := params[len(params)-1].(error); ok {
			cause = err
		}
	}
	if len(pairs) == 0 {
		return template, cause
	}
	return strings.NewReplacer(pairs...).Replace(template), cause
}
