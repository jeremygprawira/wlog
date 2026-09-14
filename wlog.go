// Package wlog is a wide-event logger: one rich, structured event per unit of work
// (an HTTP request, a job, a script) instead of scattered log lines. Code enriches the
// event through a context.Context as work progresses; the event is redacted and
// written out exactly once, when the unit of work ends.
//
// Read top to bottom: New builds a *Logger from options. WithContext attaches it to a
// context.Context so package-level functions (Start, Set, ...) can find it. Start
// begins one event and returns the end func that emits it.
package wlog

import (
	"context"

	"github.com/jeremygprawira/wlog/redact"
)

// Logger holds the configuration every event is built and emitted with. Build one with
// New at startup; attach it to request/job contexts with WithContext.
type Logger struct {
	redactor *redact.Redactor
	service  serviceInfo
}

type serviceInfo struct {
	name, version, env string
}

// Option configures a Logger built by New.
type Option func(*Logger)

// New builds a Logger from opts. With no options, it uses redact.Default() and no
// service metadata.
func New(opts ...Option) *Logger {
	l := &Logger{redactor: redact.Default()}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// WithService sets the service.name/version/env fields every event carries.
func WithService(name, version, env string) Option {
	return func(l *Logger) { l.service = serviceInfo{name: name, version: version, env: env} }
}

// WithRedactor sets the *redact.Redactor used to mask every event before it is
// written. Unset, New uses redact.Default().
func WithRedactor(r *redact.Redactor) Option {
	return func(l *Logger) { l.redactor = r }
}

type loggerCtxKey struct{}

// WithContext attaches l to ctx so Start (and everything else in this package) can
// find it.
func (l *Logger) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, l)
}

func loggerFrom(ctx context.Context) *Logger {
	l, _ := ctx.Value(loggerCtxKey{}).(*Logger)
	return l
}
