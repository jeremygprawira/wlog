package wlog

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// maxErrorList caps how many earlier errors one event keeps in errors[] (gate G4).
const maxErrorList = 10

// maxCauses caps how many causes one error lists, so a wide join stays bounded.
const maxCauses = 16

// ErrorInfo is the detail Error stores for one error, in the shape every sink emits.
// An ErrorExtractor fills this in from whatever error type the caller's error library
// uses, so core never needs to know about that library.
type ErrorInfo struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Status  int    `json:"status,omitempty"`
	Cause   string `json:"cause,omitempty"`
	Stack   string `json:"stack,omitempty"`
	// Type is the Go type of the error, such as "*foo.barError".
	Type string `json:"type,omitempty"`
	// Causes lists the messages of an errors.Join or a multi-%w error, capped at
	// maxCauses entries so a wide join never grows an event without bound.
	Causes []string       `json:"causes,omitempty"`
	Why    string         `json:"why,omitempty"`
	Fix    string         `json:"fix,omitempty"`
	Link   string         `json:"link,omitempty"`
	Attrs  map[string]any `json:"attrs,omitempty"`
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
	info := ErrorInfo{Code: "INTERNAL", Message: err.Error(), Type: typeName(err)}

	// An error library can expose a stable code through a Code method, in the
	// string form or the any form. errors.As finds the method through any
	// wrapping, so a coded error inside a fmt.Errorf still reports its code. core
	// reads the method and never the type, which keeps it library-agnostic.
	var coded interface{ Code() string }
	if stderrors.As(err, &coded) {
		if code := coded.Code(); code != "" {
			info.Code = code
		}
	}
	var codedAny interface{ Code() any }
	if stderrors.As(err, &codedAny) {
		if value := codedAny.Code(); value != nil {
			info.Code = fmt.Sprint(value)
		}
	}

	// A joined error lists every cause it holds, and a single wrap keeps its one
	// cause in cause. errors.Unwrap returns the first cause of a chain, while an
	// Unwrap method that returns a slice names them all.
	if cause := stderrors.Unwrap(err); cause != nil {
		info.Cause = cause.Error()
	}
	info.Causes = causesOf(err, 0)

	// Any error can opt into carrying its own stack this way — http-std's
	// recovered panics do — without core needing to know about panics.
	var stacked interface{ Stack() string }
	if stderrors.As(err, &stacked) {
		info.Stack = stacked.Stack()
	}
	// pkg/errors and cockroachdb/errors hand out program counters from a
	// StackTrace method. Reflection reads them with no import of either library.
	if info.Stack == "" {
		info.Stack = stackFromFrames(err)
	}
	return info
}

// causesOf returns the messages of every cause an error holds, capped at
// maxCauses and maxCauseDepth.
//
// A multi-error, which errors.Join and a double %w produce, exposes its causes
// through an Unwrap method that returns a slice. Each cause is walked in turn, so
// a join of joins still lists the leaves a reader needs.
func causesOf(err error, depth int) []string {
	if depth > 8 {
		return nil
	}
	multi, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil
	}
	var out []string
	for _, cause := range multi.Unwrap() {
		if cause == nil {
			continue
		}
		if len(out) >= maxCauses {
			break
		}
		out = append(out, cause.Error())
		out = append(out, causesOf(cause, depth+1)...)
		if len(out) > maxCauses {
			out = out[:maxCauses]
		}
	}
	return out
}

// frameReader is the method pkg/errors uses to carry a stack.
type frameReader interface {
	StackTrace() []uintptr
}

// stackFromFrames renders the program counters an error carries.
//
// The counters arrive through an interface, so core reads them with reflection
// and never imports the library that produced them. An error without the method,
// or one whose counters resolve to no frame, gives an empty string.
func stackFromFrames(err error) string {
	var reader frameReader
	if !stderrors.As(err, &reader) {
		return ""
	}
	frames := runtime.CallersFrames(reader.StackTrace())
	var b strings.Builder
	for {
		frame, more := frames.Next()
		if frame.Function != "" {
			fmt.Fprintf(&b, "%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line)
		}
		if !more || b.Len() > 8<<10 {
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
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
