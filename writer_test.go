// This file tests the event writer: the default writer never blocks the emitting
// goroutine, it drops the oldest line when its queue is full, Flush and Close drain the
// queue, and WriterSync writes where the event ended.
package wlog_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
)

// blockedWriter blocks every write until the test closes release.
type blockedWriter struct{ release chan struct{} }

// Write waits for the release channel, then accepts the line.
func (w *blockedWriter) Write(p []byte) (int, error) {
	<-w.release
	return len(p), nil
}

// lockedBuffer is a buffer that a writer goroutine and a test may share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends one line.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what the writer goroutine has written so far.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// slowWriter writes one line at a time, so a test can prove that Flush waits.
type slowWriter struct{ wrote atomic.Int64 }

// Write sleeps, then counts the line.
func (w *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(time.Millisecond)
	w.wrote.Add(1)
	return len(p), nil
}

// emitMany runs count minimal events through one logger, and returns how long they took.
func emitMany(ctx context.Context, count int) time.Duration {
	start := time.Now()
	for i := 0; i < count; i++ {
		_, end := wlog.Start(ctx, "op")
		end()
	}
	return time.Since(start)
}

// TestWriter_CORE20_BlockedWriterNeverBlocks proves that a backend which blocks costs
// the emitting goroutine nothing, so an event never waits for a console or a file.
//
// The race detector inflates the emit path about ten times, so this test compares a
// blocked writer with a fast one, and it checks the one-second budget of CORE-20 only
// outside that build (make test rather than make race).
func TestWriter_CORE20_BlockedWriterNeverBlocks(t *testing.T) {
	quiet := wlog.OnProblem(func(wlog.Problem) {})

	blocked := &blockedWriter{release: make(chan struct{})}
	blockedLog := wlog.New(wlog.WithWriter(blocked), quiet)
	blockedTook := emitMany(blockedLog.WithContext(context.Background()), 10_000)
	close(blocked.release)

	fastLog := wlog.New(wlog.WithWriter(&lockedBuffer{}), quiet)
	fastTook := emitMany(fastLog.WithContext(context.Background()), 10_000)

	if blockedTook > fastTook+250*time.Millisecond {
		t.Errorf("a blocked writer cost %s, and a fast writer %s", blockedTook, fastTook)
	}
	if !raceEnabled && blockedTook > time.Second {
		t.Errorf("10,000 emits took %s with a blocked writer, want under 1 second", blockedTook)
	}
}

// TestWriter_DropsReported proves that a full queue drops the oldest line, counts it,
// and reports WLOG_WRITER_DROPPED.
func TestWriter_DropsReported(t *testing.T) {
	problems := make(chan wlog.Problem, 4)
	blocked := &blockedWriter{release: make(chan struct{})}
	defer close(blocked.release)
	log := wlog.New(
		wlog.WithWriter(blocked, wlog.WriterBuffer(1)),
		wlog.OnProblem(func(p wlog.Problem) { problems <- p }),
	)

	for i := 0; i < 100; i++ {
		_, end := wlog.Start(log.WithContext(context.Background()), "op")
		end()
	}
	if got := log.Stats().WriterDropped; got == 0 {
		t.Error("WriterDropped = 0, want the lines the full queue dropped")
	}
	select {
	case p := <-problems:
		if p.Code != "WLOG_WRITER_DROPPED" {
			t.Errorf("code = %q, want WLOG_WRITER_DROPPED", p.Code)
		}
	default:
		t.Error("a dropped line reported nothing")
	}
}

// TestWriter_FlushDrainsQueue proves that Flush waits until every queued line reached
// the backend, so a caller reads its own output at shutdown.
func TestWriter_FlushDrainsQueue(t *testing.T) {
	out := &slowWriter{}
	log := wlog.New(wlog.WithWriter(out), wlog.WithFormat(wlog.FormatJSON))

	for i := 0; i < 3; i++ {
		_, end := wlog.Start(log.WithContext(context.Background()), "op")
		end()
	}
	if err := log.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := out.wrote.Load(); got != 3 {
		t.Errorf("the writer received %d lines after Flush, want 3", got)
	}
}

// TestWriter_SyncOption proves that WriterSync writes the line on the goroutine that
// ended the event, with no queue behind it.
func TestWriter_SyncOption(t *testing.T) {
	out := &lockedBuffer{}
	log := wlog.New(
		wlog.WithWriter(out, wlog.WriterSync()),
		wlog.WithFormat(wlog.FormatJSON),
	)

	_, end := wlog.Start(log.WithContext(context.Background()), "op")
	end()

	if !strings.Contains(out.String(), `"operation":"op"`) {
		t.Errorf("WriterSync left the line unwritten when end returned: %q", out.String())
	}
	if got := log.Stats().WriterDropped; got != 0 {
		t.Errorf("WriterDropped = %d, want 0 with a synchronous writer", got)
	}
}

// TestWriter_SilentNoDrain proves that WithSilent with no drain reports
// WLOG_SILENT_NO_DRAIN, because that Logger drops every event.
func TestWriter_SilentNoDrain(t *testing.T) {
	problems := make(chan wlog.Problem, 2)
	wlog.New(wlog.WithSilent(), wlog.OnProblem(func(p wlog.Problem) { problems <- p }))

	select {
	case p := <-problems:
		if p.Code != "WLOG_SILENT_NO_DRAIN" {
			t.Errorf("code = %q, want WLOG_SILENT_NO_DRAIN", p.Code)
		}
	default:
		t.Error("WithSilent with no drain reported nothing")
	}
}
