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
	if info.Message == "" {
		info.Message = entry.Message
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
	return info
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
