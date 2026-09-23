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
	"io"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremygprawira/wlog"
)

// Sender is what pipeline.Wrap needs from a real backend: send a batch, report
// failure. A phase-4 drain (Axiom, Loki, ...) implements this, not wlog.Drain
// directly — Wrap is what turns a Sender into a wlog.Drain.
type Sender interface {
	SendBatch(ctx context.Context, events []map[string]any) error
}

type wrapped struct {
	next Sender
	cfg  config

	mu         sync.Mutex
	buf        []map[string]any
	oldestTime time.Time

	closed    atomic.Bool
	closeOnce sync.Once
	// Counters for Stats. They are atomic because the worker goroutine writes
	// them and a caller reads them.
	sent     atomic.Int64
	dropped  atomic.Int64
	retries  atomic.Int64
	batches  atomic.Int64
	wake     chan struct{}
	closeSig chan struct{}
	done     chan struct{}
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
		wake:     make(chan struct{}, 1),
		closeSig: make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

// Send buffers event and returns immediately; it never calls next itself and never
// blocks (gate G3). A full buffer drops the oldest queued event.
func (w *wrapped) Send(ctx context.Context, event map[string]any) {
	if !w.admits(event) {
		// A filtered event is not a lost one: the caller asked for this, the same way
		// core's own level filter works. Nothing counts it as a drop.
		return
	}
	if w.closed.Load() {
		// A closed worker never sends again, so the event is reported rather than
		// buffered where nothing will read it.
		w.dropped.Add(1)
		w.reportDrop([]map[string]any{event}, errClosed)
		return
	}
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.oldestTime = time.Now()
	}
	var dropped map[string]any
	if len(w.buf) >= w.cfg.maxBuffer {
		dropped = w.buf[0]
		w.buf = w.buf[1:]
	}
	w.buf = append(w.buf, event)
	w.mu.Unlock()

	// Wake the worker, so it recomputes when this event is due instead of sleeping for
	// the rest of the old batch interval.
	w.signal()

	if dropped != nil {
		// The report runs after the unlock, so a callback may call Send again
		// without deadlocking on this lock, and the counter says an event was lost.
		w.dropped.Add(1)
		w.reportDrop([]map[string]any{dropped}, nil)
	}
}

// admits reports whether an event passes MinLevel.
//
// An audit event always passes, because a filtered audit fact is a hole in a chain a
// reader must be able to verify. An event with no level, or one this package does not
// know, passes too: a filter must not hide what it cannot judge.
func (w *wrapped) admits(event map[string]any) bool {
	if !w.cfg.minLevelSet {
		return true
	}
	if _, isAudit := event["audit"]; isAudit {
		return true
	}
	rank, ok := levelRanks[wlog.Level(levelOf(event))]
	if !ok {
		return true
	}
	return rank >= w.cfg.minLevel
}

// levelOf returns an event's level as a name.
func levelOf(event map[string]any) string {
	level, _ := event["level"].(string)
	return level
}

// signal wakes the worker without blocking. A send that arrives while the worker is
// busy leaves one token, so the worker looks again when it returns.
func (w *wrapped) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *wrapped) run() {
	timer := time.NewTimer(w.nextWake())
	defer timer.Stop()
	for {
		select {
		case <-w.wake:
			// Stop the old timer, so the reset below uses the newest oldest-time.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		case <-w.closeSig:
			w.flushAll(context.Background())
			close(w.done)
			return
		}
		w.flushIfReady()
		timer.Reset(w.nextWake())
	}
}

// nextWake returns how long the worker may sleep before it must look at the
// buffer again.
//
// An empty buffer sleeps for the batch interval, and a buffered event sleeps only
// until the oldest one is due. The worker therefore wakes when there is work, rather
// than on a fixed poll that either lags behind a short interval or spins on a long
// one.
func (w *wrapped) nextWake() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return w.cfg.batchInterval
	}
	if len(w.buf) >= w.cfg.batchSize {
		// A full batch leaves at once, so the wait is the shortest one.
		return time.Millisecond
	}
	due := w.cfg.batchInterval - time.Since(w.oldestTime)
	return max(due, time.Millisecond)
}

// Stats returns the counters of this pipeline.
func (w *wrapped) Stats() Stats {
	w.mu.Lock()
	buffered := len(w.buf)
	w.mu.Unlock()
	return Stats{
		Sent:     w.sent.Load(),
		Dropped:  w.dropped.Load(),
		Retries:  w.retries.Load(),
		Buffered: buffered,
		Batches:  w.batches.Load(),
	}
}

// Dropped returns how many events the pipeline lost.
func (w *wrapped) Dropped() int { return int(w.dropped.Load()) }

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
	// A batch holds at most BatchSize, so a burst of events arrives in several
	// batches rather than one that a backend refuses for its size.
	if len(w.buf) > w.cfg.batchSize {
		batch := w.buf[:w.cfg.batchSize]
		w.buf = w.buf[w.cfg.batchSize:]
		return batch
	}
	batch := w.buf
	w.buf = nil
	return batch
}

// errClosed is the reason a Send reports when the pipeline already closed.
var errClosed = errors.New("pipeline: closed")

// RetryError lets a Sender's error say more than "failed": whether it is even worth
// retrying, and how long to wait if the server said so (e.g. a 429's Retry-After).
// pipeline/httpdrain's *StatusError implements this; a plain error just gets the
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
	w.batches.Add(1)
	for attempt := 1; attempt <= w.cfg.maxAttempts; attempt++ {
		if attempt > 1 {
			w.retries.Add(1)
		}
		if err = w.trySendBatch(ctx, batch); err == nil {
			w.sent.Add(int64(len(batch)))
			return
		}
		var re RetryError
		var pe *PartialError
		if errors.As(err, &pe) {
			// A partial batch names its own outcomes: the worker reports the refused
			// events and tries again with the rest. An empty rest ends the batch.
			batch = w.dropPartial(batch, pe)
			if len(batch) == 0 {
				return
			}
		} else if errors.As(err, &re) && !re.Retryable() {
			break
		}
		if attempt < w.cfg.maxAttempts {
			delay := w.retryDelay(attempt)
			if errors.As(err, &re) {
				if ra := re.RetryAfter(); ra > 0 {
					// A server may ask for any wait, and a Retry-After of hours
					// would park the worker, so the cap applies here too.
					delay = min(ra, w.cfg.maxDelay)
				}
			}
			if !w.wait(ctx, delay) {
				// The worker is closing, so this batch stops here rather than
				// holding the shutdown open for the whole wait.
				w.dropped.Add(int64(len(batch)))
				w.reportDrop(batch, err)
				return
			}
		}
	}
	w.dropped.Add(int64(len(batch)))
	w.reportDrop(batch, err)
}

// dropPartial handles a PartialError: it reports the events the backend refused for
// good, counts them, and returns the events to send again.
//
// An index outside the batch names nothing, so the worker ignores it. An index in both
// lists counts as dropped, because a permanent refusal wins over a retry. An event in
// neither list reached the backend, so it counts as sent.
func (w *wrapped) dropPartial(batch []map[string]any, pe *PartialError) []map[string]any {
	dropped := make([]bool, len(batch))
	for _, i := range pe.Dropped {
		if i >= 0 && i < len(batch) {
			dropped[i] = true
		}
	}
	retry := make([]bool, len(batch))
	for _, i := range pe.Retry {
		if i >= 0 && i < len(batch) && !dropped[i] {
			retry[i] = true
		}
	}

	var refused []map[string]any
	var again []map[string]any
	for i := range batch {
		switch {
		case dropped[i]:
			refused = append(refused, batch[i])
		case retry[i]:
			again = append(again, batch[i])
		}
	}
	if len(refused) > 0 {
		w.dropped.Add(int64(len(refused)))
		w.reportDrop(refused, pe)
	}
	if accepted := len(batch) - len(refused) - len(again); accepted > 0 {
		w.sent.Add(int64(accepted))
	}
	return again
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
//
// The cap comes before the jitter, so the wait a caller sees never passes
// MaxDelay, and the doubling stops at 1<<30, which no duration can overflow.
func (w *wrapped) retryDelay(attempt int) time.Duration {
	var d time.Duration
	switch w.cfg.backoff {
	case Linear:
		d = w.cfg.initialDelay * time.Duration(attempt)
	case Fixed:
		d = w.cfg.initialDelay
	default: // Exponential
		shift := min(attempt-1, 30)
		if shift < 0 {
			shift = 0
		}
		d = w.cfg.initialDelay * time.Duration(1<<uint(shift))
	}
	if d > w.cfg.maxDelay {
		d = w.cfg.maxDelay
	}
	if d <= 0 {
		return 0
	}
	// The jitter stays inside the cap, so the wait never exceeds it.
	jitter := time.Duration(rand.Int63n(int64(d)/5 + 1))
	return min(d+jitter, w.cfg.maxDelay)
}

// wait sleeps for d and reports whether the wait finished.
//
// The wait selects on the worker's own context, so a Close that carries a deadline
// stops a retry in flight instead of waiting for a backoff to end.
func (w *wrapped) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-w.closeSig:
		return false
	case <-ctx.Done():
		return false
	}
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

// Flush sends every buffered event now and keeps the worker running, so a caller
// that is about to lose its process (a Lambda freeze, a container stop) can push
// what it holds without ending the drain.
func (w *wrapped) Flush(ctx context.Context) error {
	if w.closed.Load() {
		return nil
	}
	w.mu.Lock()
	batch := w.buf
	w.buf = nil
	w.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	w.sendBatch(ctx, batch)
	return nil
}

// Close stops the background goroutine after flushing every buffered event, then closes
// the Sender when it holds something the process must release, such as a file. It returns
// ctx's error if its deadline passes first.
func (w *wrapped) Close(ctx context.Context) error {
	w.closed.Store(true)
	w.closeOnce.Do(func() { close(w.closeSig) })
	select {
	case <-w.done:
		return w.closeSender(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closeSender releases the Sender's own resource, if it has one. Without this a file
// drain would never sync or close its file, because the pipeline, not the caller, owns
// the last write.
func (w *wrapped) closeSender(ctx context.Context) error {
	if closer, ok := w.next.(senderCloser); ok {
		return closer.Close(ctx)
	}
	if closer, ok := w.next.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Setup gives the wrapped Sender the configured Logger when it wants one, so a Sender can
// report its own faults through Logger.Report. A Sender without Setup is left alone.
func (w *wrapped) Setup(l *wlog.Logger) error {
	if s, ok := w.next.(interface{ Setup(*wlog.Logger) error }); ok {
		return s.Setup(l)
	}
	return nil
}

// senderCloser is the optional Close a Sender may implement, so Wrap can release a file,
// a connection, or a client pool after the last batch.
type senderCloser interface {
	Close(ctx context.Context) error
}
