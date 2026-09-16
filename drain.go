package wlog

import (
	"context"
	"fmt"
)

// Drain receives every emitted event, already redacted, alongside the built-in
// stdout sink. This is the one interface every backend (a file, Axiom, Loki, a
// test recorder, ...) implements to plug into wlog.
//
// The map belongs to core, and it is final: a drain must not change it, and it
// must not keep a reference and change it later. Two drains receive the same
// event, so a write from one of them would be a change the other never saw. Copy
// what you need to keep.
type Drain interface {
	Send(ctx context.Context, event map[string]any)
}

// drainCloser is the optional interface a Drain can also implement so Logger.Close
// flushes or disconnects it. Checked with a type assertion, never required.
type drainCloser interface {
	Close(ctx context.Context) error
}

// DrainFunc adapts a plain function to the Drain interface, the same way http.HandlerFunc
// adapts a function to http.Handler.
type DrainFunc func(ctx context.Context, event map[string]any)

// Send calls f.
func (f DrainFunc) Send(ctx context.Context, event map[string]any) { f(ctx, event) }

// WithDrains adds drains every event is sent to, alongside the default stdout sink.
func WithDrains(drains ...Drain) Option {
	return func(l *Logger) { l.drains = append(l.drains, drains...) }
}

// OnError is called when a drain panics. Default: errors are dropped rather than
// panicking or blocking the caller (gate G3); set this to observe them.
func OnError(fn func(err error, source string)) Option {
	return func(l *Logger) { l.onError = fn }
}

// sendToDrains fans event out to every configured drain. Each call is isolated: a
// panicking drain is recovered and reported via OnError, and never stops the other
// drains or affects the event's caller.
func (l *Logger) sendToDrains(ctx context.Context, event map[string]any) {
	for _, d := range l.drains {
		l.safeSend(ctx, d, event)
	}
}

func (l *Logger) safeSend(ctx context.Context, d Drain, event map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			l.reportError(fmt.Errorf("panic: %v", r), sourceName(d))
		}
	}()
	d.Send(ctx, event)
}

// reportError hands one failure to OnError.
//
// OnError is user code, and a panic inside it would otherwise climb out of a
// logging call. The panic is swallowed here, because the report of a failure
// must never become a failure of its own.
func (l *Logger) reportError(err error, source string) {
	if l.onError == nil {
		return
	}
	defer func() { _ = recover() }()
	l.onError(err, source)
}

// Close calls Close(ctx) on every drain that implements it (drainCloser), stopping at
// ctx's deadline. It returns the first error encountered, if any, but always attempts
// every drain regardless — one slow or failing drain does not skip the rest.
func (l *Logger) Close(ctx context.Context) error {
	var firstErr error
	for _, d := range l.drains {
		c, ok := d.(drainCloser)
		if !ok {
			continue
		}
		if err := l.safeClose(ctx, c); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (l *Logger) safeClose(ctx context.Context, c drainCloser) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			l.reportError(err, sourceName(c))
		}
	}()
	return c.Close(ctx)
}
