package wlog

import "github.com/jeremygprawira/wlog/redact"

// SetRedactor atomically swaps the *redact.Redactor every later event is masked with.
// In-flight events already past the redact stage are unaffected; every one after this
// call sees exactly next (or redact.Default(), if next is nil) — never a mix of the
// two. Safe to call from any goroutine, concurrently with emitting events.
func (l *Logger) SetRedactor(next *redact.Redactor) {
	if next == nil {
		next = redact.Default()
	}
	l.redactor.Store(next)
}

// WithRedactFingerprint controls whether every event carries redact.fingerprint, a
// short hash of the redactor that masked it (useful for auditing which denylist was
// active for a given log line). Default true.
func WithRedactFingerprint(enabled bool) Option {
	return func(l *Logger) { l.redactFingerprint = enabled }
}

func (l *Logger) currentRedactor() *redact.Redactor {
	if r := l.redactor.Load(); r != nil {
		return r
	}
	return redact.Default()
}
