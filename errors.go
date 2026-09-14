package wlog

import (
	"context"
	stderrors "errors"
)

// maxErrorList caps how many earlier errors one event keeps in errors[] (gate G4).
const maxErrorList = 10

// ErrorInfo is the detail Error stores for one error, in the shape every sink emits.
// An ErrorExtractor fills this in from whatever error type the caller's error library
// uses, so core never needs to know about that library.
type ErrorInfo struct {
	Code    string         `json:"code,omitempty"`
	Message string         `json:"message,omitempty"`
	Kind    string         `json:"kind,omitempty"`
	Status  int            `json:"status,omitempty"`
	Cause   string         `json:"cause,omitempty"`
	Stack   string         `json:"stack,omitempty"`
	Why     string         `json:"why,omitempty"`
	Fix     string         `json:"fix,omitempty"`
	Link    string         `json:"link,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// ErrorExtractor adapts an error library's error type into an ErrorInfo. Plug in an
// implementation for herr, a custom type, or anything else with WithErrorExtractor;
// the zero value New() uses is defaultExtractor, which works with any plain error.
type ErrorExtractor interface {
	Extract(err error) ErrorInfo
}

type defaultExtractor struct{}

// Extract gives every plain error a usable, safe-to-log fallback: an "INTERNAL" code,
// its Error() text as the message, and its unwrapped cause if it has one.
func (defaultExtractor) Extract(err error) ErrorInfo {
	info := ErrorInfo{Code: "INTERNAL", Message: err.Error()}
	if cause := stderrors.Unwrap(err); cause != nil {
		info.Cause = cause.Error()
	}
	return info
}

// WithErrorExtractor sets how Error turns an error into an ErrorInfo. Default
// defaultExtractor{}.
func WithErrorExtractor(x ErrorExtractor) Option {
	return func(l *Logger) { l.errorExtractor = x }
}

// Error records err on the current event via the Logger's ErrorExtractor. The
// previous error, if any, moves to errors[] (capped at maxErrorList); this call's
// error becomes the new error field. Unless SetLevel already ran, the event's level
// becomes LevelError — an explicit SetLevel, before or after, still wins.
func Error(ctx context.Context, err error) {
	if err == nil {
		return
	}
	e := eventFrom(ctx)
	if e == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sealed {
		e.recordLateWrite()
		return
	}

	info := e.extractor.Extract(err)
	if e.errInfo != nil {
		if len(e.errList) >= maxErrorList {
			e.dropped++
		} else {
			e.errList = append(e.errList, *e.errInfo)
		}
	}
	e.errInfo = &info
	if !e.levelSet {
		e.level = LevelError
	}
}
