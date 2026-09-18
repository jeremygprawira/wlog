// This file holds the event writer: it decides where one rendered line goes and when.
// The default writer is async, because a backend that blocks must never block the
// request that ended the event (CORE-20).
package wlog

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// defaultWriterBuffer is how many lines the async queue holds before it drops the
// oldest one.
const defaultWriterBuffer = 4096

// writerConfig holds how a caller wants the writer to run.
type writerConfig struct {
	sync   bool // write on the emitting goroutine
	buffer int  // lines the async queue holds
}

// WriterOption configures the event writer.
type WriterOption func(*writerConfig)

// WriterSync writes each line on the goroutine that ended the event, so the caller sees
// the line before the next call returns. Use it for a test, a batch job, or a short
// script.
func WriterSync() WriterOption { return func(c *writerConfig) { c.sync = true } }

// WriterBuffer sets how many lines the async queue holds before it drops the oldest
// line. A fresh line is worth more than a stale one, so the newest line always wins.
func WriterBuffer(events int) WriterOption {
	return func(c *writerConfig) {
		if events > 0 {
			c.buffer = events
		}
	}
}

// WithWriter sends the rendered line of every event to w. The default is os.Stdout, and
// the default writer is async. WriterSync writes on the emitting goroutine instead, and
// WriterBuffer sets the size of the async queue.
func WithWriter(w io.Writer, opts ...WriterOption) Option {
	return func(l *Logger) {
		if w != nil {
			l.writer.out = w
		}
		for _, o := range opts {
			o(&l.writer.cfg)
		}
	}
}

// writeItem is one entry of the writer queue: a rendered line and the destination it
// goes to, or a marker that lets a caller wait for the lines before it.
type writeItem struct {
	out  io.Writer // the destination, resolved on the emitting goroutine
	line []byte
	done chan struct{} // non-nil for a flush or a stop marker
	stop bool          // with done, tells the writer goroutine to exit
}

// eventWriter writes one rendered line per event.
//
// The async form holds a bounded queue and one goroutine, so a blocked backend never
// blocks the emitting goroutine (CORE-20). The sync form writes the line where it is
// handed over. A caller chooses between them with WriterSync at New.
//
// ponytail: the queue is counted in lines, so its worst case is the queue size times the
// largest event, plus about 229 KiB of fixed queue space. Add a byte budget if a service
// emits events near the 256 KiB cap.
type eventWriter struct {
	cfg       writerConfig
	out       io.Writer
	owner     *Logger
	queue     chan writeItem
	mu        sync.Mutex // held while the sync form writes one line
	closeOnce sync.Once
	wg        sync.WaitGroup
	dropped   atomic.Int64
	stopped   atomic.Bool
	reported  atomic.Bool
}

// start prepares the writer. New calls it once, after every option has run, so a later
// WriterSync or WriterBuffer still wins.
func (w *eventWriter) start(l *Logger) {
	w.owner = l
	if w.cfg.sync || l.silent {
		return
	}
	size := w.cfg.buffer
	if size <= 0 {
		size = defaultWriterBuffer
	}
	w.queue = make(chan writeItem, size)
	w.wg.Add(1)
	go w.run()
}

// run writes queued lines until it reads a stop marker.
func (w *eventWriter) run() {
	defer w.wg.Done()
	for item := range w.queue {
		if item.done != nil {
			close(item.done)
			if item.stop {
				return
			}
			continue
		}
		w.writeLine(item.out, item.line)
	}
}

// writeLine writes one rendered line, and reports a backend that refuses it.
func (w *eventWriter) writeLine(out io.Writer, line []byte) {
	if _, err := out.Write(line); err != nil {
		w.owner.reportProblem(codeDrainFailed, "stdout", err)
	}
}

// destination returns the place a line goes. The emitting goroutine resolves it, so the
// writer goroutine never reads os.Stdout and a test that swaps os.Stdout stays race free.
func (w *eventWriter) destination() io.Writer {
	if w.out != nil {
		return w.out
	}
	return os.Stdout
}

// write hands one rendered line to the writer.
//
// The sync form writes it here. The async form queues it, and a full queue drops its
// oldest line, so the emitting goroutine never waits for the backend (gate G3).
func (w *eventWriter) write(line []byte) {
	out := w.destination()
	if w.queue == nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.writeLine(out, line)
		return
	}
	if w.stopped.Load() {
		return
	}
	for {
		select {
		case w.queue <- writeItem{out: out, line: line}:
			return
		default:
		}
		// The queue is full. Take the oldest line out and try again, so this loop
		// always makes room and always ends.
		select {
		case <-w.queue:
			w.dropOne()
		default:
		}
	}
}

// dropOne counts one line the queue could not hold, and reports the first drop once.
//
// The report happens once, because a stuck backend is the one case where a report per
// drop would call user code on the emit path thousands of times. Stats holds every
// count.
func (w *eventWriter) dropOne() {
	w.dropped.Add(1)
	if w.reported.CompareAndSwap(false, true) {
		w.owner.reportProblem(codeWriterDropped, "writer",
			fmt.Errorf("the queue was full, so the oldest line was dropped"))
	}
}

// flush waits until every line queued so far is written, or until ctx ends.
func (w *eventWriter) flush(ctx context.Context) error {
	if w.queue == nil || w.stopped.Load() {
		return nil
	}
	done := make(chan struct{})
	select {
	case w.queue <- writeItem{done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop drains the queue and ends the writer goroutine.
func (w *eventWriter) stop(ctx context.Context) error {
	if w.queue == nil {
		return nil
	}
	var err error
	w.closeOnce.Do(func() {
		err = w.flush(ctx)
		done := make(chan struct{})
		select {
		case w.queue <- writeItem{done: done, stop: true}:
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
		// The stop marker comes after every queued line, so the writer goroutine
		// reaches it only once the queue is empty.
		select {
		case <-done:
		case <-ctx.Done():
			err = ctx.Err()
			return
		}
		w.stopped.Store(true)
		w.wg.Wait()
	})
	return err
}
