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
	prefix  string
	entries map[string]Entry // keyed by full code
	short   map[string]string
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
	r := &Registry{prefix: p, entries: map[string]Entry{}, short: map[string]string{}}
	for _, entry := range entries {
		short := strings.ToUpper(strings.TrimSpace(entry.Code))
		if short == "" {
			panic("catalog: entry code is empty")
		}
		full := fullCode(p, short)
		if _, duplicate := r.entries[full]; duplicate {
			panic("catalog: duplicate code " + full)
		}
		copied := entry
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

// Codes returns every full code, sorted.
func (r *Registry) Codes() []string {
	codes := make([]string, 0, len(r.entries))
	for code := range r.entries {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// Get resolves a full or a short code to its entry.
func (r *Registry) Get(code string) (Entry, bool) {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if entry, ok := r.entries[upper]; ok {
		return entry, true
	}
	if full, ok := r.short[upper]; ok {
		return r.entries[full], true
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
		prefix:  r.prefix,
		full:    fullCode(r.prefix, entry.Code),
		message: message,
		entry:   entry,
		cause:   cause,
	}
}

// fullCode joins a prefix and a short code into the wire code.
func fullCode(prefix, short string) string {
	return strings.ToUpper(prefix) + "_" + strings.ToUpper(short)
}

// render fills {name} placeholders from key, value pairs. A placeholder with no pair
// stays as written, so a missing value never panics. It also returns the cause.
func render(template string, params []any) (string, error) {
	var cause error
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
		template = strings.ReplaceAll(template, "{"+key+"}", fmt.Sprint(value))
	}
	if len(params)%2 == 1 {
		if err, ok := params[len(params)-1].(error); ok {
			cause = err
		}
	}
	return template, cause
}
