// Package wlogherr adapts github.com/jeremygprawira/herr errors into wlog.ErrorInfo,
// following the mapping in docs/SPEC-errors-herr.md.
package wlogherr

import (
	"encoding/json"

	"github.com/jeremygprawira/herr"
	"github.com/jeremygprawira/wlog"
)

// Option configures Extractor.
type Option func(*config)

type config struct {
	publicMessage bool
}

// WithPublicMessage falls back to herr's public-facing message when Internal is empty.
// Default: Message only ever comes from the internal (log-only) detail, since herr's
// public text is meant for end users, not logs.
func WithPublicMessage() Option {
	return func(c *config) { c.publicMessage = true }
}

var kindNames = map[herr.Kind]string{
	herr.KindInternal:      "internal",
	herr.KindInvalid:       "invalid",
	herr.KindUnauthorized:  "unauthorized",
	herr.KindForbidden:     "forbidden",
	herr.KindNotFound:      "not_found",
	herr.KindConflict:      "conflict",
	herr.KindRateLimited:   "rate_limited",
	herr.KindTimeout:       "timeout",
	herr.KindUnavailable:   "unavailable",
	herr.KindUnprocessable: "unprocessable",
}

type extractorFunc func(err error) wlog.ErrorInfo

func (f extractorFunc) Extract(err error) wlog.ErrorInfo { return f(err) }

// Extractor adapts herr errors to wlog.ErrorInfo via herr.LogRecord's mapping.
func Extractor(opts ...Option) wlog.ErrorExtractor {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	return extractorFunc(func(err error) wlog.ErrorInfo {
		rec := herr.LogRecord(err)
		info := wlog.ErrorInfo{
			Code:    rec.Code,
			Kind:    kindNames[rec.Kind],
			Status:  rec.HTTPStatus,
			Message: rec.Internal,
			Stack:   rec.Stack,
		}
		if rec.Cause != nil {
			info.Cause = rec.Cause.Error()
		}
		if info.Message == "" && cfg.publicMessage {
			info.Message = publicMessage(err)
		}
		if len(rec.Fields) > 0 {
			info.Attrs = make(map[string]any, len(rec.Fields))
			for _, f := range rec.Fields {
				info.Attrs[f.Key] = f.Val
			}
		}
		return info
	})
}

// publicMessage reads herr's public message via its allow-listed JSON wire shape
// (json.Marshal(err) → *herr.Error.MarshalJSON), since the wire DTO type is unexported.
func publicMessage(err error) string {
	b, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		return ""
	}
	var wire struct {
		Message string `json:"message"`
	}
	if jsonErr := json.Unmarshal(b, &wire); jsonErr != nil {
		return ""
	}
	return wire.Message
}
