// This file holds the problem layer: the Problem type, the handler that receives
// reports, and the reporter that folds repeats. Core, a drain, or a plugin reports a
// fault once, and the reporter decides when the caller hears about it.
package wlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Problem is one fault that wlog reports to the caller. Code is stable, so a caller
// can match on it. Message is one sentence and never holds an event value, because a
// report about an event must not leak that event.
type Problem struct {
	Code    string // stable, such as WLOG_DRAIN_FAILED
	Source  string // the hook, drain, or option that reported it
	Message string // one sentence, never holding an event value
	Why     string // why the code exists
	Fix     string // what the caller does about it
	Link    string // the section of docs/problems.md that explains the code
	Err     error  // the failure behind the report, if any
	Count   int    // reports folded into this one since the last delivery
}

// ProblemHandler receives every Problem that the rate limit allows. A handler that
// panics is ignored, so a report never breaks the caller.
type ProblemHandler func(Problem)

// OnProblem sets the handler that receives every reported Problem. Without it, the
// default handler writes one JSON line to standard error per code per minute.
func OnProblem(fn ProblemHandler) Option {
	return func(l *Logger) { l.problems.setHandler(fn) }
}

// Problems returns the catalog of codes wlog reports. A caller reads it to document
// the codes, and a test reads it to prove that docs/problems.md covers each one.
func Problems() []Problem {
	out := make([]Problem, len(problemCatalog))
	copy(out, problemCatalog)
	return out
}

// Report sends one problem to the handler. A drain, a plugin, or an output preset
// outside core calls this to report its own fault. Report fills in Why, Fix, and Link
// from the catalog, so a caller may set only Code and Source.
func (l *Logger) Report(p Problem) {
	if l == nil {
		return
	}
	l.problems.deliver(l.fill(p))
}

// fill adds the catalog text of a known code, so a caller may report a bare code.
func (l *Logger) fill(p Problem) Problem {
	known, ok := problemByCode(p.Code)
	if !ok {
		return p
	}
	if p.Why == "" {
		p.Why = known.Why
	}
	if p.Fix == "" {
		p.Fix = known.Fix
	}
	if p.Link == "" {
		p.Link = known.Link
	}
	return p
}

// reportProblem reports one fault with a catalog code. The message comes from err, so
// a handler that ignores Err still reads what happened.
func (l *Logger) reportProblem(code, source string, err error) {
	p := Problem{Code: code, Source: source}
	if err != nil {
		p.Message = err.Error()
		p.Err = err
	}
	l.problems.deliver(l.fill(p))
}

// problemReporter counts reports per code and hands each one to the handler. It holds
// the handler and the counts, so a Logger needs no package state. The rate limit lives
// in the default handler, because a caller that sets its own handler wants every fault.
type problemReporter struct {
	mu      sync.Mutex
	handler ProblemHandler
	counts  map[string]int // reports per code since the Logger was built
	pending []Problem      // reports that arrived before a handler was set
}

// newProblemReporter returns a reporter with no handler yet, so a report that arrives
// during New waits for the handler a caller passes.
func newProblemReporter() *problemReporter {
	return &problemReporter{counts: map[string]int{}}
}

// setHandler installs fn and delivers the reports that waited for a handler.
func (r *problemReporter) setHandler(fn ProblemHandler) {
	r.mu.Lock()
	if fn != nil {
		r.handler = fn
	}
	pending := r.pending
	r.pending = nil
	r.mu.Unlock()
	for _, p := range pending {
		r.deliver(p)
	}
}

// deliver counts one report and hands it to the handler. A report that arrives before
// a handler exists waits in the queue.
func (r *problemReporter) deliver(p Problem) {
	r.mu.Lock()
	if r.handler == nil {
		r.pending = append(r.pending, p)
		r.mu.Unlock()
		return
	}
	r.counts[p.Code]++
	p.Count = r.counts[p.Code]
	handler := r.handler
	r.mu.Unlock()
	callProblemHandler(handler, p)
}

// flush installs the default handler when the caller set none, and delivers the
// reports that waited. New calls it once every option has run.
func (r *problemReporter) flush() {
	r.mu.Lock()
	empty := r.handler == nil
	r.mu.Unlock()
	if empty {
		r.setHandler(defaultProblemHandler(os.Stderr))
		return
	}
	r.setHandler(nil)
}

// callProblemHandler runs one handler under recover, so a panicking handler never
// breaks the emit path.
func callProblemHandler(handler ProblemHandler, p Problem) {
	defer func() { _ = recover() }()
	handler(p)
}

// defaultProblemHandler writes one JSON line to w, and it writes at most one line per
// code per minute, so a hot loop cannot flood the console. The line carries count, so
// the reader learns how often the code fired.
func defaultProblemHandler(w io.Writer) ProblemHandler {
	var mu sync.Mutex
	last := map[string]time.Time{}
	return func(p Problem) {
		mu.Lock()
		seen := last[p.Code]
		if !seen.IsZero() && time.Since(seen) < time.Minute {
			mu.Unlock()
			return
		}
		last[p.Code] = time.Now()
		mu.Unlock()
		line := map[string]any{"code": p.Code}
		put := func(key, value string) {
			if value != "" {
				line[key] = value
			}
		}
		put("source", p.Source)
		put("message", p.Message)
		put("why", p.Why)
		put("fix", p.Fix)
		put("link", p.Link)
		if p.Err != nil {
			line["error"] = p.Err.Error()
		}
		if p.Count > 1 {
			line["count"] = p.Count
		}
		body, err := json.Marshal(line)
		if err != nil {
			return
		}
		// A console that refuses the line is not worth a second report, because the
		// report of a failure must not become a failure of its own.
		_, _ = fmt.Fprintln(w, string(body))
	}
}
