// Package pipeline wraps a batch-sending backend with batching, retry, and a bounded
// buffer, so a real drain (Axiom, Loki, a file) never slows down or blocks the request
// that logged through it, and never grows memory without bound.
//
// Read top to bottom: Sender is what a real backend implements; Wrap turns one into a
// wlog.Drain whose Send never blocks — it appends to an internal buffer that a
// background goroutine (started by Wrap, stopped by Close) drains into batches.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/jeremygprawira/wlog"
)

// Sender is what pipeline.Wrap needs from a real backend: send a batch, report
// failure. A phase-4 drain (Axiom, Loki, ...) implements this, not wlog.Drain
// directly — Wrap is what turns a Sender into a wlog.Drain.
type Sender interface {
	SendBatch(ctx context.Context, events []map[string]any) error
}

// pollInterval is how often the background goroutine checks whether the current
// buffer is ready to flush. Small relative to any realistic BatchInterval, so it adds
// negligible latency without polling wastefully.
const pollInterval = 5 * time.Millisecond

type wrapped struct {
	next Sender
	cfg  config

	mu         sync.Mutex
	buf        []map[string]any
	oldestTime time.Time

	closeOnce sync.Once
	closeSig  chan struct{}
	done      chan struct{}
}

// Wrap returns a wlog.Drain backed by next, batching and buffering per opts. The
// returned Drain also implements Close(ctx context.Context) error, picked up by
// wlog.Logger.Close.
func Wrap(next Sender, opts ...Option) wlog.Drain {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	w := &wrapped{
		next:     next,
		cfg:      c,
		closeSig: make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

// Send buffers event and returns immediately; it never calls next itself and never
// blocks (gate G3). A full buffer drops the oldest queued event.
func (w *wrapped) Send(ctx context.Context, event map[string]any) {
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.oldestTime = time.Now()
	}
	if len(w.buf) >= w.cfg.maxBuffer {
		dropped := w.buf[0]
		w.buf = w.buf[1:]
		if w.cfg.onDropped != nil {
			w.cfg.onDropped([]map[string]any{dropped}, nil)
		}
	}
	w.buf = append(w.buf, event)
	w.mu.Unlock()
}

func (w *wrapped) run() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.flushIfReady()
		case <-w.closeSig:
			w.flushAll(context.Background())
			close(w.done)
			return
		}
	}
}

// flushIfReady sends the current buffer as one batch once it has reached BatchSize or
// BatchInterval has passed since its oldest event, whichever comes first.
func (w *wrapped) flushIfReady() {
	batch := w.takeBatchIfReady()
	if batch == nil {
		return
	}
	w.sendBatch(context.Background(), batch)
}

func (w *wrapped) takeBatchIfReady() []map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return nil
	}
	ready := len(w.buf) >= w.cfg.batchSize || time.Since(w.oldestTime) >= w.cfg.batchInterval
	if !ready {
		return nil
	}
	batch := w.buf
	w.buf = nil
	return batch
}

// RetryError lets a Sender's error say more than "failed": whether it is even worth
// retrying, and how long to wait if the server said so (e.g. a 429's Retry-After).
// internal/httpdrain's *StatusError implements this; a plain error just gets the
// normal backoff for every attempt.
type RetryError interface {
	error
	Retryable() bool
	RetryAfter() time.Duration
}

// sendBatch tries next.SendBatch up to MaxAttempts times, waiting between tries per
// the configured backoff curve (or a RetryError's own RetryAfter, if it gives one).
// A RetryError that reports Retryable() == false stops immediately, since retrying it
// verbatim would just fail the same way. If every attempt fails, the batch is dropped
// and OnDropped(batch, lastErr) is called.
func (w *wrapped) sendBatch(ctx context.Context, batch []map[string]any) {
	var err error
	for attempt := 1; attempt <= w.cfg.maxAttempts; attempt++ {
		if err = w.trySendBatch(ctx, batch); err == nil {
			return
		}
		var re RetryError
		if errors.As(err, &re) && !re.Retryable() {
			break
		}
		if attempt < w.cfg.maxAttempts {
			delay := w.retryDelay(attempt)
			if errors.As(err, &re) {
				if ra := re.RetryAfter(); ra > 0 {
					delay = ra
				}
			}
			time.Sleep(delay)
		}
	}
	w.reportDrop(batch, err)
}

// trySendBatch calls the next Sender under recover.
//
// A Sender is user code, and a backend library that panics on a malformed body must
// not take the process with it. The panic becomes an ordinary error, so the retry
// policy and the drop report treat it like any other failure.
func (w *wrapped) trySendBatch(ctx context.Context, batch []map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return w.next.SendBatch(ctx, batch)
}

// reportDrop tells the caller that a batch is gone, under recover.
//
// OnDropped is user code too, and a panic inside it would otherwise climb out of the
// worker goroutine and kill the process. It runs without the buffer lock, so a
// callback may call Send again.
func (w *wrapped) reportDrop(batch []map[string]any, err error) {
	if w.cfg.onDropped == nil {
		return
	}
	defer func() { _ = recover() }()
	w.cfg.onDropped(batch, err)
}

// retryDelay is the wait before the (attempt+1)th try, per Backoff, capped at
// MaxDelay, plus up to 20% jitter to avoid a thundering herd across many drains.
func (w *wrapped) retryDelay(attempt int) time.Duration {
	var d time.Duration
	switch w.cfg.backoff {
	case Linear:
		d = w.cfg.initialDelay * time.Duration(attempt)
	case Fixed:
		d = w.cfg.initialDelay
	default: // Exponential
		d = w.cfg.initialDelay * time.Duration(1<<uint(attempt-1))
	}
	if d > w.cfg.maxDelay {
		d = w.cfg.maxDelay
	}
	if d <= 0 {
		return 0
	}
	return d + time.Duration(rand.Int63n(int64(d)/5+1))
}

// flushAll drains and sends everything left in the buffer, for Close.
func (w *wrapped) flushAll(ctx context.Context) {
	w.mu.Lock()
	batch := w.buf
	w.buf = nil
	w.mu.Unlock()
	if len(batch) > 0 {
		w.sendBatch(ctx, batch)
	}
}

// Close stops the background goroutine after flushing every buffered event, or
// returns ctx's error if its deadline passes first.
func (w *wrapped) Close(ctx context.Context) error {
	w.closeOnce.Do(func() { close(w.closeSig) })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
