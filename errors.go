package wlog

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
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
	// Data is safe to send back to a client, such as a rejected field name.
	Data map[string]any `json:"data,omitempty"`
	// Internal is log-only detail, such as a row id or a query.
	Internal map[string]any `json:"internal,omitempty"`
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
	// An error library can expose a stable code through a Code() string method. core
	// reads the method and never the type, so it stays library-agnostic. A plain error
	// keeps INTERNAL.
	if coded, ok := err.(interface{ Code() string }); ok {
		if code := coded.Code(); code != "" {
			info.Code = code
		}
	}
	if cause := stderrors.Unwrap(err); cause != nil {
		info.Cause = cause.Error()
	}
	// Any error can opt into carrying its own stack this way — http-std's recovered
	// panics do — without core needing to know about panics specifically.
	if s, ok := err.(interface{ Stack() string }); ok {
		info.Stack = s.Stack()
	}
	return info
}

// DefaultExtractor returns the extractor New uses when WithErrorExtractor is not set.
// A decorator, such as catalog.Extractor, wraps it.
func DefaultExtractor() ErrorExtractor { return defaultExtractor{} }

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
	if isNilError(err) {
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

	info := e.safeExtract(err)
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

// isNilError reports whether an error is absent.
//
// A nil error interface is the common case, and a typed nil pointer satisfies the
// interface while it holds no value. The second one panics inside any method it
// reaches, so it counts as absent here.
func isNilError(err error) bool {
	if err == nil {
		return true
	}
	v := reflect.ValueOf(err)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// safeExtract runs the extractor for one error and returns its result.
//
// An extractor is user code, and user code panics. The panic becomes the
// INTERNAL fallback with a readable message, and OnError hears about it, so a
// broken extractor never reaches the caller of Error and never loses the event.
func (e *event) safeExtract(err error) (info ErrorInfo) {
	defer func() {
		if r := recover(); r != nil {
			info = ErrorInfo{
				Code:    "INTERNAL",
				Message: fmt.Sprintf("[extractor: %T panicked]", err),
			}
			if l := loggerFrom(e.ctx); l != nil {
				l.reportError(fmt.Errorf("extractor panic: %v", r), "extractor")
			}
		}
	}()
	return e.extractor.Extract(err)
}

// ErrorData returns the current event error's Data map, or nil when the current event
// has no error. A transport uses it to decide what part of an error is safe to send
// back to a client.
func ErrorData(ctx context.Context) map[string]any {
	e := eventFrom(ctx)
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errInfo == nil {
		return nil
	}
	return e.errInfo.Data
}

// Errorf builds an error from a format string and its arguments, records it through
// Error, and returns that same error value. It replaces the build-then-record pair
// that a handler writes today.
func Errorf(ctx context.Context, format string, a ...any) error {
	err := fmt.Errorf(format, a...)
	Error(ctx, err)
	return err
}
