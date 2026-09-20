// Package wlogcockroach extracts cockroachdb/errors errors into wlog.ErrorInfo: the
// telemetry key, the domain, the HTTP code, the redacted message, the first reportable
// stack, the issue link, and the safe details. It never copies a sensitive value.
package wlogcockroach

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/exthttp"
	"github.com/cockroachdb/redact"

	"github.com/jeremygprawira/wlog"
)

// Option configures the extractor.
type Option func(*options)

// options holds the resolved settings of one extractor.
type options struct {
	unredacted bool
	hints      bool
	details    bool
}

// WithUnredactedMessage uses the raw error text as the message. The default is the
// redacted text, which hides the values that the library marked as unsafe.
func WithUnredactedMessage() Option {
	return func(o *options) { o.unredacted = true }
}

// WithHints adds the flattened hints as the fix line. Hints can hold personal data, so
// the default leaves the fix empty.
func WithHints() Option {
	return func(o *options) { o.hints = true }
}

// WithDetails adds the error details as internal detail. Details can hold personal data,
// so the default leaves them out.
func WithDetails() Option {
	return func(o *options) { o.details = true }
}

// Extractor returns the wlog ErrorExtractor for cockroachdb/errors errors. Pass it to
// wlog.WithErrorExtractor.
func Extractor(opts ...Option) wlog.ErrorExtractor {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return extractor{cfg: cfg}
}

// extractor reads one cockroachdb/errors error into an ErrorInfo.
type extractor struct {
	cfg options
}

// Extract fills ErrorInfo from one error. The message is redacted unless
// WithUnredactedMessage is set, so a value the library marked as unsafe never reaches the
// event.
func (e extractor) Extract(err error) wlog.ErrorInfo {
	if err == nil {
		return wlog.ErrorInfo{}
	}
	info := wlog.ErrorInfo{
		Status: exthttp.GetHTTPCode(err, 0),
		Type:   fmt.Sprintf("%T", err),
	}
	if keys := telemetryKeys(err); len(keys) > 0 {
		info.Code = keys[0]
	}
	if domain := errors.GetDomain(err); domain != errors.NoDomain {
		info.Kind = string(domain)
	}
	if e.cfg.unredacted {
		info.Message = err.Error()
	} else {
		info.Message = string(redact.Sprint(err).Redact())
	}
	info.Stack = stackOf(err)
	if links := errors.GetAllIssueLinks(err); len(links) > 0 {
		info.Link = links[0].IssueURL
	}
	if e.cfg.hints {
		info.Fix = errors.FlattenHints(err)
	}

	internal := map[string]any{}
	if keys := telemetryKeys(err); len(keys) > 0 {
		internal["telemetry_keys"] = keys
	}
	if details := safeDetails(err); len(details) > 0 {
		internal["safe_details"] = details
	}
	if e.cfg.details {
		if details := errors.GetAllDetails(err); len(details) > 0 {
			internal["details"] = details
		}
	}
	if len(internal) > 0 {
		info.Internal = internal
	}
	return info
}

// telemetryKeys returns the telemetry keys of the whole chain, sorted.
func telemetryKeys(err error) []string {
	keys := errors.GetTelemetryKeys(err)
	sort.Strings(keys)
	return keys
}

// safeDetails returns the safe details of the whole chain without the stack entry, which
// the stack field already holds.
func safeDetails(err error) []any {
	payloads := errors.GetAllSafeDetails(err)
	details := make([]any, 0, len(payloads))
	for _, payload := range payloads {
		if strings.Contains(payload.OriginalTypeName, "stack") {
			continue
		}
		details = append(details, map[string]any{
			"type":    payload.OriginalTypeName,
			"details": payload.SafeDetails,
		})
	}
	return details
}

// stackOf returns the first reportable stack in the chain, as text.
func stackOf(err error) string {
	for current := err; current != nil; current = errors.UnwrapOnce(current) {
		stack := errors.GetReportableStackTrace(current)
		if stack == nil || len(stack.Frames) == 0 {
			continue
		}
		var text strings.Builder
		for _, frame := range stack.Frames {
			fmt.Fprintf(&text, "%s\n\t%s:%d\n", frame.Function, frame.Filename, frame.Lineno)
		}
		return strings.TrimRight(text.String(), "\n")
	}
	return ""
}
