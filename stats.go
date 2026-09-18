// This file holds the runtime stats of one Logger: the events it emitted, the events
// it dropped and why, and one entry per drain that reports its own numbers. A debug
// route reads them through DebugHandler, and `wlog doctor --url` reads the same JSON.
package wlog

import (
	"fmt"
	"sync/atomic"
	"time"
)

// The reason an event was dropped. Debug mode reports it as the source of
// WLOG_EVENT_DROPPED, and Stats counts it. A sampler or a filter drops an event for one
// of these reasons and for no other.
const (
	dropSampled  = "sampled"   // head sampling or a Keeper dropped it
	dropLevel    = "level"     // the minimum level filter dropped it
	dropDisabled = "disabled"  // logging is off for the whole process
	dropClosed   = "closed"    // the Logger was closed
	dropTooLarge = "too_large" // the event was over the size cap, even after a trim
)

// drainSlowThreshold is the time a drain may spend on the emitting goroutine. Past it,
// core reports WLOG_DRAIN_SLOW once, because a slow drain blocks the request (gate G3).
const drainSlowThreshold = 5 * time.Millisecond

// Stats is one snapshot of what a Logger did. Emitted counts the events that reached
// the drains and the writers. Dropped counts the events that did not, by reason.
type Stats struct {
	Emitted       int64            `json:"emitted"`
	Dropped       map[string]int64 `json:"dropped,omitempty"`
	WriterDropped int64            `json:"writer_dropped"`
	Drains        []DrainStats     `json:"drains,omitempty"`
}

// DrainStats holds what one drain reports about itself: its queue, what it sent, what
// it dropped, how often it retried, and its last error as text.
type DrainStats struct {
	Name      string `json:"name"`
	Queued    int64  `json:"queued"`
	Sent      int64  `json:"sent"`
	Dropped   int64  `json:"dropped"`
	Retried   int64  `json:"retried"`
	LastError string `json:"last_error,omitempty"`
}

// StatsReporter is the optional interface a Drain implements so Stats can show its
// queue. Core never needs it, and a drain without it costs Stats nothing.
type StatsReporter interface {
	Stats() DrainStats
}

// loggerStats holds the counters of one Logger. Each drop reason has its own atomic, so
// a drop is counted without a lock and without a map on the emit path (gate G3).
type loggerStats struct {
	emitted       atomic.Int64
	writerDropped atomic.Int64
	sampled       atomic.Int64
	level         atomic.Int64
	disabled      atomic.Int64
	closed        atomic.Int64
	tooLarge      atomic.Int64
}

// count records one drop under reason, and returns nothing, so a caller may ignore it.
func (s *loggerStats) count(reason string) {
	switch reason {
	case dropSampled:
		s.sampled.Add(1)
	case dropLevel:
		s.level.Add(1)
	case dropDisabled:
		s.disabled.Add(1)
	case dropClosed:
		s.closed.Add(1)
	case dropTooLarge:
		s.tooLarge.Add(1)
	}
}

// snapshot returns the drops by reason, leaving out a reason that never fired.
func (s *loggerStats) snapshot() map[string]int64 {
	out := map[string]int64{}
	for reason, count := range map[string]int64{
		dropSampled:  s.sampled.Load(),
		dropLevel:    s.level.Load(),
		dropDisabled: s.disabled.Load(),
		dropClosed:   s.closed.Load(),
		dropTooLarge: s.tooLarge.Load(),
	} {
		if count > 0 {
			out[reason] = count
		}
	}
	return out
}

// Stats returns a snapshot of what this Logger did, including one entry per drain that
// implements StatsReporter.
func (l *Logger) Stats() Stats {
	return Stats{
		Emitted:       l.stats.emitted.Load(),
		Dropped:       l.stats.snapshot(),
		WriterDropped: l.stats.writerDropped.Load(),
		Drains:        l.drainStats(),
	}
}

// drainStats reads the numbers of every drain that reports its own, each under recover,
// so a broken drain cannot break a debug route.
func (l *Logger) drainStats() []DrainStats {
	out := make([]DrainStats, 0, len(l.drains))
	for _, d := range l.drains {
		if s, ok := l.drainStat(d); ok {
			out = append(out, s)
		}
	}
	return out
}

// drainStat runs one drain's Stats hook under recover, and names the drain when the
// hook leaves the name out.
func (l *Logger) drainStat(d Drain) (out DrainStats, ok bool) {
	r, ok := d.(StatsReporter)
	if !ok {
		return DrainStats{}, false
	}
	defer func() {
		if rec := recover(); rec != nil {
			l.reportProblem(codeHookPanic, sourceName(d), fmt.Errorf("panic: %v", rec))
			out, ok = DrainStats{}, false
		}
	}()
	out = r.Stats()
	if out.Name == "" {
		out.Name = sourceName(d)
	}
	return out, true
}

// dropEvent counts one dropped event, and in debug mode reports why the event is
// missing (BET-10). A debug report never holds an event value, so a report about an
// event cannot leak it.
func (l *Logger) dropEvent(reason string) {
	l.stats.count(reason)
	if !l.debug {
		return
	}
	l.Report(Problem{
		Code:    codeEventDropped,
		Source:  reason,
		Message: "the event was dropped before a writer saw it",
	})
}

// slowDrain reports a drain that blocked the emitting goroutine, once per drain, so a
// hot loop over a slow backend reports one line and not one line per event.
func (l *Logger) slowDrain(d Drain, took time.Duration) {
	name := sourceName(d)
	if _, seen := l.slowDrains.LoadOrStore(name, struct{}{}); seen {
		return
	}
	l.reportProblem(codeDrainSlow, name,
		fmt.Errorf("Send took %s on the emitting goroutine", took))
}
