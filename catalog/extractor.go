package catalog

import (
	"fmt"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// extractor decorates one wlog.ErrorExtractor with a set of registries.
type extractor struct {
	next       wlog.ErrorExtractor
	registries []*Registry
}

// Extractor decorates next. It fills Kind, Status, Why, Fix, and Link from the entry whose
// full code matches the ErrorInfo.Code that next produced. A field next already filled
// wins, so a per-request detail is never replaced by a static default. The decorator reads
// the code and never the Go type, so it is agnostic to the error library behind next.
//
// It matches full codes: a registry resolves a short code only after AllowShortCodes. A nil
// registry is skipped. It returns an error when two registries define the same full code,
// because the answer would otherwise depend on the order of the arguments.
func Extractor(next wlog.ErrorExtractor, registries ...*Registry) (wlog.ErrorExtractor, error) {
	if next == nil {
		return nil, fmt.Errorf("catalog: Extractor needs a next extractor")
	}
	if err := checkDuplicates(registries); err != nil {
		return nil, err
	}
	return &extractor{next: next, registries: registries}, nil
}

// MustExtractor is Extractor, but panics on a duplicate code or a missing next extractor.
// Use it in a package-level variable or main.
func MustExtractor(next wlog.ErrorExtractor, registries ...*Registry) wlog.ErrorExtractor {
	e, err := Extractor(next, registries...)
	if err != nil {
		panic(err)
	}
	return e
}

// checkDuplicates reports a full code that more than one registry defines. A nil registry
// holds no codes and is skipped.
func checkDuplicates(registries []*Registry) error {
	owner := map[string]string{}
	for _, registry := range registries {
		if registry == nil {
			continue
		}
		for code := range registry.entries {
			if first, seen := owner[code]; seen {
				return fmt.Errorf("catalog: code %s is defined by two registries (%s and %s)", code, first, registry.prefix)
			}
			owner[code] = registry.prefix
		}
	}
	return nil
}

// Extract runs next, then fills the empty fields from the matching entry.
func (e *extractor) Extract(err error) wlog.ErrorInfo {
	info := e.next.Extract(err)
	if info.Code == "" {
		return info
	}
	entry, ok := e.lookup(info.Code)
	if !ok {
		return info
	}
	if info.Kind == "" {
		info.Kind = entry.Kind
	}
	if info.Status == 0 {
		info.Status = entry.Status
	}
	if info.Why == "" {
		info.Why = entry.Why
	}
	if info.Fix == "" {
		info.Fix = entry.Fix
	}
	if info.Link == "" {
		info.Link = entry.Link
	}
	// "Message" is deliberately not filled: the entry holds a template, not a message, and
	// the wrapped extractor's own message describes this error far better than a static one
	// would. Copying the raw template in was worse than leaving it out.
	info.Attrs = withDomain(info.Attrs, entry.domain)
	info.Data = mergeDefaults(info.Data, entry.Data)
	info.Internal = mergeDefaults(info.Internal, entry.Internal)
	return info
}

// withDomain records which domain the code belongs to, under the key SPEC.md names. A
// value the wrapped extractor already set wins.
func withDomain(attrs map[string]any, domain string) map[string]any {
	if domain == "" {
		return attrs
	}
	if _, exists := attrs["domain"]; exists {
		return attrs
	}
	out := make(map[string]any, len(attrs)+1)
	for key, value := range attrs {
		out[key] = value
	}
	out["domain"] = domain
	return out
}

// mergeDefaults returns defaults under the values a per-request extractor already filled,
// so a static default never replaces a real detail. Both maps are new: the registry's own
// maps are never shared with an event.
func mergeDefaults(filled, defaults map[string]any) map[string]any {
	if len(defaults) == 0 {
		return filled
	}
	out := make(map[string]any, len(filled)+len(defaults))
	for key, value := range filled {
		out[key] = value
	}
	for key, value := range defaults {
		if _, exists := out[key]; !exists {
			out[key] = copyValue(value)
		}
	}
	return out
}

// lookup returns the entry whose full code matches. A registry that allowed short codes
// also resolves the short spelling, and a nil registry is skipped.
func (e *extractor) lookup(code string) (Entry, bool) {
	for _, registry := range e.registries {
		if registry == nil {
			continue
		}
		if entry, ok := registry.getFull(code); ok {
			return entry, true
		}
		if !registry.allowShort {
			continue
		}
		if entry, ok := registry.Get(code); ok {
			return entry, true
		}
	}
	return Entry{}, false
}

// unusedStrings keeps the import honest when the short-code path is compiled out.
var _ = strings.TrimSpace
