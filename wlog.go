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
	"sync"
	"sync/atomic"

	"github.com/jeremygprawira/wlog/redact"
)

// disabled is the process-wide off switch behind SetEnabled. It is stored inverted,
// so the zero value means logging is on. This is the one piece of package-level state
// SPEC.md allows: it stays read-only on the hot path and holds no per-event data.
var disabled atomic.Bool

// SetEnabled turns logging on or off for the whole process. With logging off, Start
// and Detach return a no-op end func, and a plain line writes nothing. The default is
// on.
func SetEnabled(on bool) { disabled.Store(!on) }

// Enabled reports whether logging is on.
func Enabled() bool { return !disabled.Load() }

// Logger holds the configuration every event is built and emitted with. Build one with
// New at startup; attach it to request/job contexts with WithContext.
type Logger struct {
	redactor          atomic.Pointer[redact.Redactor] // swapped at runtime via SetRedactor
	redactFingerprint bool
	service           serviceInfo
	minLevel          Level
	errorExtractor    ErrorExtractor
	errorCaller       bool
	drains            []Drain
	problems          *problemReporter
	closed            atomic.Bool
	headSampler       HeadSampler
	keepers           []Keeper
	enrichers         []Enricher
	starters          []Starter
	finishers         []Finisher
	measurers         []Measurer
	stats             loggerStats
	slowDrains        sync.Map // drain names that already reported WLOG_DRAIN_SLOW
	debug             bool
	fieldNames        FieldNames
	plugins           []Plugin
	strictKeys        map[string]bool
	format            Format
	summary           func(Event) string
	silent            bool
	rawValues         bool
}

type serviceInfo struct {
	name, version, env string
}

// Option configures a Logger built by New.
type Option func(*Logger)

// New builds a Logger from opts. With no options, it uses redact.Default() and no
// service metadata.
func New(opts ...Option) *Logger {
	l := &Logger{
		minLevel: LevelDebug, errorExtractor: defaultExtractor{}, errorCaller: true,
		redactFingerprint: true, problems: newProblemReporter(),
	}
	l.redactor.Store(redact.Default())
	warnings := l.applyEnvDefaults()
	for _, opt := range opts {
		opt(l)
	}
	l.wirePlugins()
	l.setupDrains()
	for _, w := range warnings {
		l.reportProblem(codeInvalidConfig, w.source, w.err)
	}
	// A report that arrived before OnProblem ran waits for the handler, and the
	// default handler takes over when the caller set none.
	l.problems.flush()
	return l
}

// WithDebug turns on debug mode: core reports WLOG_EVENT_DROPPED with the reason every
// time it drops an event, so a reader learns why an event is missing (BET-10). The env
// var WLOG_DEBUG=1 does the same.
func WithDebug(on bool) Option { return func(l *Logger) { l.debug = on } }

// WithSilent drops the stdout write while keeping every stage and every drain. A
// service that ships events to a backend alone wants no duplicate console output.
func WithSilent() Option { return func(l *Logger) { l.silent = true } }

// WithRawValues stores a value that already is a JSON tree root as given, instead of
// passing it through normalize. A struct or any other non-tree value is still
// normalized, so redaction can walk it.
func WithRawValues() Option { return func(l *Logger) { l.rawValues = true } }

// WithService sets the service.name/version/env fields every event carries.
func WithService(name, version, env string) Option {
	return func(l *Logger) {
		// An empty argument keeps whatever the environment gave, so a caller may
		// pass only the field it knows.
		l.service = serviceInfo{
			name:    pick(name, l.service.name),
			version: pick(version, l.service.version),
			env:     pick(env, l.service.env),
		}
	}
}

// pick returns value when it is not empty, and fallback otherwise.
func pick(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// WithRedactor sets the *redact.Redactor used to mask every event before it is
// written. Unset, New uses redact.Default().
func WithRedactor(r *redact.Redactor) Option {
	return func(l *Logger) {
		if r == nil {
			// Resolve the default once, here, so an event never rebuilds it.
			r = redact.Default()
		}
		l.redactor.Store(r)
	}
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
