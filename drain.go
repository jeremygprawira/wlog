package wlog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
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
// disconnects it. Checked with a type assertion, never required.
type drainCloser interface {
	Close(ctx context.Context) error
}

// drainFlusher is the optional interface a Drain can also implement so
// Logger.Flush pushes its buffer out. A flush never stops the drain.
type drainFlusher interface {
	Flush(ctx context.Context) error
}

// DrainFunc adapts a plain function to the Drain interface, the same way http.HandlerFunc
// adapts a function to http.Handler.
type DrainFunc func(ctx context.Context, event map[string]any)

// Send calls f.
func (f DrainFunc) Send(ctx context.Context, event map[string]any) { f(ctx, event) }

// WithDrains adds drains that every event is sent to, alongside the default stdout
// sink.
//
// The drains of one event run in order, on the goroutine that ends it, so a
// custom drain that blocks also blocks the request. The built-in network drains
// hand their send to a background pipeline, which keeps a slow backend off that
// path.
func WithDrains(drains ...Drain) Option {
	return func(l *Logger) { l.drains = append(l.drains, drains...) }
}

// sendToDrains fans event out to every configured drain. Each call is isolated: a
// panicking drain is recovered and reported via OnProblem, and never stops the other
// drains or affects the event's caller.
func (l *Logger) sendToDrains(ctx context.Context, event map[string]any) {
	for _, d := range l.drains {
		l.safeSend(ctx, d, event)
	}
}

func (l *Logger) safeSend(ctx context.Context, d Drain, event map[string]any) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeDrainFailed, sourceName(d), fmt.Errorf("panic: %v", r))
		}
	}()
	d.Send(ctx, event)
	// A drain runs on the emitting goroutine, so a slow one blocks the request. Report
	// the first one once, so a slow backend is visible without a line per event.
	if took := time.Since(start); took > drainSlowThreshold {
		l.slowDrain(d, took)
	}
}

// Flush pushes the buffer of every drain that implements drainFlusher (a batched
// pipeline, a file writer) and keeps them running, so the next event still arrives. It
// also waits until every line the writer has queued reaches the backend. It returns the
// first error, and it always attempts every drain.
func (l *Logger) Flush(ctx context.Context) error {
	var firstErr error
	if err := l.writer.flush(ctx); err != nil {
		firstErr = err
	}
	for _, d := range l.drains {
		f, ok := d.(drainFlusher)
		if !ok {
			continue
		}
		if err := l.safeFlush(ctx, f); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Close closes every drain that implements drainCloser at the same time, and it
// returns once they all finish or once ctx ends. One slow drain never delays the
// others, and a drain that cannot finish inside ctx does not hold the process.
//
// An event that arrives after Close sends nothing and reports through OnError,
// because a closed drain would drop it silently.
func (l *Logger) Close(ctx context.Context) error {
	l.closed.Store(true)
	if err := l.writer.stop(ctx); err != nil {
		l.reportProblem(codeDrainFailed, "writer", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for _, d := range l.drains {
		c, ok := d.(drainCloser)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(c drainCloser) {
			defer wg.Done()
			if err := l.safeClose(ctx, c); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(c)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		// The deadline came first, so a drain is still running. Say so rather
		// than report a clean close.
		l.reportProblem(codeDrainFailed, "drains", fmt.Errorf("close: %w", ctx.Err()))
		mu.Lock()
		defer mu.Unlock()
		return errors.Join(firstErr, ctx.Err())
	}

	mu.Lock()
	defer mu.Unlock()
	return firstErr
}

// safeFlush runs one Flush under recover.
func (l *Logger) safeFlush(ctx context.Context, f drainFlusher) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			l.reportProblem(codeDrainFailed, sourceName(f), err)
		}
	}()
	return f.Flush(ctx)
}

func (l *Logger) safeClose(ctx context.Context, c drainCloser) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			l.reportProblem(codeDrainFailed, sourceName(c), err)
		}
	}()
	return c.Close(ctx)
}
