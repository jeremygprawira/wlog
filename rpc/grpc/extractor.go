// This file holds the error extractor of the gRPC adapter. It maps one status and its
// details into the ErrorInfo of an event, so a reader sees the code, the message, the why,
// the fix, the link, and the safe data.
package wloggrpc

import (
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	"github.com/jeremygprawira/wlog"
)

// Extractor returns the extractor that reads a gRPC status. Configure it on the Logger
// with WithErrorExtractor, and every status error carries its details into the event.
//
//	// The error code, the message, the why, the fix, the link, and the data
//	log := wlog.New(wlog.WithErrorExtractor(wloggrpc.Extractor()))
func Extractor() wlog.ErrorExtractor { return grpcExtractor{} }

// grpcExtractor reads a status error into an ErrorInfo.
type grpcExtractor struct{}

// Extract maps one error. A plain error keeps the default reading, because only a status
// error carries details.
func (grpcExtractor) Extract(err error) wlog.ErrorInfo {
	info := wlog.DefaultExtractor().Extract(err)
	if err == nil {
		return info
	}
	st, ok := status.FromError(err)
	if !ok {
		st = status.FromContextError(err)
	}
	info.Code = st.Code().String()
	info.Message = st.Message()
	info.Why = st.Message()

	data := map[string]any{}
	fix := ""
	for _, detail := range st.Details() {
		switch d := detail.(type) {
		case *errdetails.ErrorInfo:
			if d.Reason != "" {
				info.Code = d.Reason
			}
			if d.Domain != "" {
				data["domain"] = d.Domain
			}
			if len(d.Metadata) > 0 {
				data["metadata"] = d.Metadata
			}
		case *errdetails.LocalizedMessage:
			if d.Message != "" {
				info.Message = d.Message
			}
		case *errdetails.Help:
			for _, link := range d.Links {
				if link.Url != "" {
					info.Link = link.Url
					break
				}
			}
		case *errdetails.BadRequest:
			if violations := fieldNames(d); len(violations) > 0 {
				data["field_violations"] = violations
				fix = "Send a valid value for each field the error names"
			}
		case *errdetails.RetryInfo:
			if d.RetryDelay != nil {
				data["retry_delay_ms"] = float64(d.RetryDelay.AsDuration().Microseconds()) / 1000
				fix = "Retry the call after the delay the error names"
			}
		case *errdetails.DebugInfo:
			// Never read: a debug detail can hold a stack and internal names.
		}
	}
	if fix != "" {
		info.Fix = fix
	}
	if len(data) > 0 {
		info.Data = data
	}
	return info
}

// fieldNames returns the name of each rejected field, and no value, because a violation
// description can hold the value the caller sent.
func fieldNames(b *errdetails.BadRequest) []any {
	out := make([]any, 0, len(b.FieldViolations))
	for _, violation := range b.FieldViolations {
		if violation.Field == "" {
			continue
		}
		out = append(out, map[string]any{"field": violation.Field})
	}
	return out
}
