package pipeline

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremygprawira/wlog"
)

// fanOutQueue is how many events one drain's queue holds. A full queue drops the
// newest event rather than blocking the caller, so memory stays bounded (G4).
const fanOutQueue = 256

// FanOut returns a wlog.Drain that delivers every event to every drain.
//
// Read top to bottom: Send queues the event for each drain, and one worker goroutine
// per drain reads that queue and calls the drain. So a slow, hung, or panicking drain
// never delays the others, never blocks the caller, and never costs more than one
// goroutine. The result also implements Flush and Close, which each forward to every
// drain that implements them, so a Logger.Flush or Logger.Close reaches a pipeline.Wrap
// held inside the FanOut.
//
// Drains receive the event map read-only, and they run at the same time. A drain that
// must change the event copies it first, per the core drain contract.
func FanOut(drains ...wlog.Drain) wlog.Drain {
	f := &fanOut{
		drains: drains,
		queues: make([]chan queued, len(drains)),
		stop:   make(chan struct{}),
		idle:   make(chan struct{}),
	}
	for i, d := range drains {
		ch := make(chan queued, fanOutQueue)
		f.queues[i] = ch
		f.wg.Add(1)
		go f.worker(d, ch)
	}
	go func() {
		f.wg.Wait()
		close(f.idle)
	}()
	return f
}

// queued is one event with the context its caller sent it with.
type queued struct {
	ctx   context.Context
	event map[string]any
}

// fanOut holds one bounded queue and one goroutine per drain.
type fanOut struct {
	drains []wlog.Drain
	queues []chan queued
	stop   chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
	// idle closes once every worker returned, so Close can wait for the queues to
	// drain without watching the WaitGroup itself.
	idle chan struct{}
	// mu keeps Send, Flush, and Close from interleaving. Send holds the read side,
	// and both Flush and Close hold the write side, so a Flush sees no half-queued
	// event and Close sets closed with no Send in flight.
	mu     sync.RWMutex
	closed bool
	// pending counts events queued but not yet delivered. The workers update it
	// without the lock, so it is atomic.
	pending atomic.Int64
}

// Send queues the event for every drain and returns immediately (gate G3). It never
// calls a drain itself, and a full queue drops that one delivery rather than waiting.
func (f *fanOut) Send(ctx context.Context, event map[string]any) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.closed {
		return
	}
	for _, ch := range f.queues {
		f.pending.Add(1)
		select {
		case ch <- queued{ctx: ctx, event: event}:
		default:
			// This drain is behind its 256-event queue, so this copy is dropped
			// instead of blocking the caller.
			f.pending.Add(-1)
		}
	}
}

// worker delivers events to one drain until stop closes and the queue is empty.
func (f *fanOut) worker(d wlog.Drain, ch chan queued) {
	defer f.wg.Done()
	for {
		select {
		case q := <-ch:
			f.deliver(d, q)
		case <-f.stop:
			// Deliver what the queue still holds, then stop. A hung drain keeps
			// this goroutine, so Close is bounded by its ctx, not by the drain.
			for {
				select {
				case q := <-ch:
					f.deliver(d, q)
				default:
					return
				}
			}
		}
	}
}

// deliver calls one drain under recover.
//
// A drain is user code, and a backend library that panics on a malformed event must
// not take the process with it (gate G3). The panic stops this one delivery, and the
// worker keeps reading its queue.
func (f *fanOut) deliver(d wlog.Drain, q queued) {
	defer func() {
		f.pending.Add(-1)
		_ = recover()
	}()
	d.Send(q.ctx, q.event)
}

// Flush waits for every queued event to reach its drain, then flushes every drain
// that implements Flush. The wait is bounded by ctx.
func (f *fanOut) Flush(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.waitIdle(ctx) {
		return ctx.Err()
	}
	return f.each("flush", func(d wlog.Drain) error {
		c, ok := d.(drainFlusher)
		if !ok {
			return nil
		}
		return c.Flush(ctx)
	})
}

// Close stops every worker after its queue drains, then closes every drain that
// implements Close. The waits are bounded by ctx.
func (f *fanOut) Close(ctx context.Context) error {
	f.mu.Lock()
	f.once.Do(func() {
		f.closed = true
		close(f.stop)
	})
	f.mu.Unlock()

	var firstErr error
	select {
	case <-f.idle:
	case <-ctx.Done():
		firstErr = ctx.Err()
	}
	// The drains still close even when a hung one used up the ctx budget, so a
	// pipeline.Wrap inside this FanOut stops rather than leaking its worker.
	if err := f.each("close", func(d wlog.Drain) error {
		c, ok := d.(drainCloser)
		if !ok {
			return nil
		}
		return c.Close(ctx)
	}); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// waitIdle reports whether every queued event reached its drain before ctx ended.
//
// A hung drain holds its own delivery forever, so this can end in ctx.Done().
func (f *fanOut) waitIdle(ctx context.Context) bool {
	if f.pending.Load() == 0 {
		return true
	}
	t := time.NewTicker(time.Millisecond)
	defer t.Stop()
	for f.pending.Load() > 0 {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
		}
	}
	return true
}

// each calls fn on every drain, under recover, and returns the first error. A drain
// that panics does not stop the others.
func (f *fanOut) each(what string, fn func(wlog.Drain) error) error {
	var firstErr error
	for _, d := range f.drains {
		if err := safeCall(d, fn); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s drain: %w", what, err)
		}
	}
	return firstErr
}

// safeCall runs fn on d and turns a panic into an error, so a broken drain never
// stops the FanOut from reaching the others.
func safeCall(d wlog.Drain, fn func(wlog.Drain) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn(d)
}

// drainCloser is the optional Close that a wrapped drain offers. FanOut forwards it
// to the drains it holds, the same way wlog.Logger does for its own drains.
type drainCloser interface {
	Close(ctx context.Context) error
}

// drainFlusher is the optional Flush that a wrapped drain offers.
type drainFlusher interface {
	Flush(ctx context.Context) error
}
