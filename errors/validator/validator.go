// Package wlogvalidator extracts go-playground/validator errors into wlog.ErrorInfo, so a
// rejected field reaches the event with its name, its tag, and its parameter, and never
// with the rejected value.
package wlogvalidator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/jeremygprawira/wlog"
)

// Option configures the extractor.
type Option func(*options)

// options holds the resolved settings of one extractor.
type options struct {
	code   string
	status int
	fixes  map[string]string
}

// WithCode names the code of a validation failure. The default is VALIDATION_FAILED.
func WithCode(code string) Option {
	return func(o *options) { o.code = code }
}

// WithStatus sets the status of a validation failure. The default is 400.
func WithStatus(status int) Option {
	return func(o *options) { o.status = status }
}

// WithFixes maps one tag to its fix line, so the event tells the caller what to do.
func WithFixes(fixes map[string]string) Option {
	return func(o *options) { o.fixes = fixes }
}

// Extractor returns the wlog ErrorExtractor for validator errors. Pass it to
// wlog.WithErrorExtractor.
func Extractor(opts ...Option) wlog.ErrorExtractor {
	cfg := options{code: "VALIDATION_FAILED", status: 400}
	for _, opt := range opts {
		opt(&cfg)
	}
	return extractor{cfg: cfg}
}

// extractor reads one validator error into an ErrorInfo.
type extractor struct {
	cfg options
}

// Extract fills ErrorInfo from one validator error. The rejected value is never read, so
// a password that failed a rule never reaches the event.
func (e extractor) Extract(err error) wlog.ErrorInfo {
	var invalid *validator.InvalidValidationError
	if errors.As(err, &invalid) {
		return wlog.ErrorInfo{
			Code: "INTERNAL", Kind: "internal", Status: 500,
			Message: err.Error(), Type: fmt.Sprintf("%T", err),
		}
	}

	var failures validator.ValidationErrors
	if !errors.As(err, &failures) {
		return wlog.ErrorInfo{}
	}

	fields := make([]any, 0, len(failures))
	internal := make([]any, 0, len(failures))
	fix := ""
	for _, failure := range failures {
		if len(fields) < 50 {
			fields = append(fields, map[string]any{
				"field": fieldName(failure.Namespace()),
				"tag":   failure.Tag(),
				"param": failure.Param(),
			})
		}
		internal = append(internal, map[string]any{
			"struct_namespace": failure.Namespace(),
			"actual_tag":       failure.ActualTag(),
			"kind":             failure.Kind().String(),
			"type":             failure.Type().String(),
		})
		if fix == "" && e.cfg.fixes != nil {
			fix = e.cfg.fixes[failure.Tag()]
		}
	}

	data := map[string]any{"fields": fields}
	if dropped := len(failures) - len(fields); dropped > 0 {
		data["fields_dropped"] = dropped
	}
	return wlog.ErrorInfo{
		Code:     e.cfg.code,
		Kind:     "validation",
		Status:   e.cfg.status,
		Message:  fmt.Sprintf("validation failed on %d fields", len(failures)),
		Fix:      fix,
		Data:     data,
		Internal: map[string]any{"fields": internal},
	}
}

// fieldName returns one field path without the root struct name.
func fieldName(namespace string) string {
	if index := strings.Index(namespace, "."); index >= 0 {
		return namespace[index+1:]
	}
	return namespace
}
