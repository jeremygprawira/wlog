// Package wlogoops extracts samber/oops errors into wlog.ErrorInfo, so an oops code,
// domain, hint, and public message reach the event. It never reads the request or the
// response of an error, because those hold headers and bodies.
package wlogoops

import (
	"errors"
	"fmt"

	"github.com/samber/oops"

	"github.com/jeremygprawira/wlog"
)

// Option configures the extractor.
type Option func(*options)

// options holds the resolved settings of one extractor.
type options struct {
	public bool
}

// WithPublicMessage uses the public message as the message, so a client-safe text reaches
// the event instead of the internal one.
func WithPublicMessage() Option {
	return func(o *options) { o.public = true }
}

// Extractor returns the wlog ErrorExtractor for oops errors. Pass it to
// wlog.WithErrorExtractor.
func Extractor(opts ...Option) wlog.ErrorExtractor {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return extractor{cfg: cfg}
}

// extractor reads one oops error into an ErrorInfo.
type extractor struct {
	cfg options
}

// Extract fills ErrorInfo from one oops error. Request() and Response() are never read,
// so an auth header or a body never reaches the event.
func (e extractor) Extract(err error) wlog.ErrorInfo {
	var oopsErr oops.OopsError
	if !errors.As(err, &oopsErr) {
		return wlog.ErrorInfo{}
	}

	message := oopsErr.Error()
	if e.cfg.public {
		message = oopsErr.Public()
	}
	info := wlog.ErrorInfo{
		Code:    codeOf(oopsErr.Code()),
		Kind:    oopsErr.Domain(),
		Message: message,
		Fix:     oopsErr.Hint(),
		Stack:   oopsErr.Stacktrace(),
		Type:    fmt.Sprintf("%T", err),
	}
	if public := oopsErr.Public(); public != "" {
		info.Data = map[string]any{"public": public}
	}
	internal := map[string]any{}
	if tags := oopsErr.Tags(); len(tags) > 0 {
		internal["tags"] = tags
	}
	if context := oopsErr.Context(); len(context) > 0 {
		internal["context"] = context
	}
	if owner := oopsErr.Owner(); owner != "" {
		internal["owner"] = owner
	}
	if span := oopsErr.Span(); span != "" {
		internal["span"] = span
	}
	if trace := oopsErr.Trace(); trace != "" {
		internal["oops_trace"] = trace
	}
	// Only the ids of the user and the tenant are kept, never their maps.
	if id, _ := oopsErr.User(); id != "" {
		internal["user_id"] = id
	}
	if id, _ := oopsErr.Tenant(); id != "" {
		internal["tenant_id"] = id
	}
	if len(internal) > 0 {
		info.Internal = internal
	}
	return info
}

// codeOf reads one oops code: a string before v1.20.0, and any value after it.
func codeOf(code any) string {
	switch value := code.(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}
