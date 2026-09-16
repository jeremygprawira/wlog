package catalog

import "github.com/jeremygprawira/wlog"

// extractor decorates one wlog.ErrorExtractor with a set of registries.
type extractor struct {
	next       wlog.ErrorExtractor
	registries []*Registry
}

// Extractor decorates next. It fills Kind, Status, Why, Fix, and Link from the entry
// whose full code matches the ErrorInfo.Code that next produced. A field next already
// filled wins, so a per-request detail is never replaced by a static default. The
// decorator reads the code and never the Go type, so it is agnostic to the error
// library behind next.
func Extractor(next wlog.ErrorExtractor, registries ...*Registry) wlog.ErrorExtractor {
	if next == nil {
		panic("catalog: Extractor needs a next extractor")
	}
	return &extractor{next: next, registries: registries}
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

// lookup returns the first entry that matches code across the registries.
func (e *extractor) lookup(code string) (Entry, bool) {
	for _, registry := range e.registries {
		if entry, ok := registry.Get(code); ok {
			return entry, true
		}
	}
	return Entry{}, false
}
