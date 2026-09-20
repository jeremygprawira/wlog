// Package work gives every non-HTTP adapter the same three calls: Start opens one unit of
// work of a kind, the Handle writes the fields of that kind, and End records the outcome
// and emits the event. Run wraps the three around a handler function.
//
// Read top to bottom: this file holds the kinds, the unit, the handle, and Run, and
// status.go holds the shared status class tables.
package work

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Kind names the shape of one unit of work. The kind decides the group of fields, the
// operation, and the summary of the event.
type Kind string

const (
	KindRequest  Kind = "request"
	KindRPC      Kind = "rpc"
	KindMessage  Kind = "message"
	KindJob      Kind = "job"
	KindCommand  Kind = "command"
	KindFunction Kind = "function"
	KindWork     Kind = "work"
)

// groups maps a kind to the group that holds its fields. KindRequest has no entry here,
// because http-core owns the http group, and kind work has none by the spec table.
var groups = map[Kind]string{
	KindRPC:      "rpc",
	KindMessage:  "messaging",
	KindJob:      "job",
	KindCommand:  "cli",
	KindFunction: "faas",
}

// Unit describes one unit of work before it starts.
type Unit struct {
	Kind      Kind
	Operation string            // empty: built from Fields by the kind table
	Fields    map[string]any    // the kind group, such as {"system": "kafka"}
	Carrier   propagate.Carrier // incoming headers, may be nil
	StartedAt time.Time         // zero: now. A message sets its enqueue time here for lag
}

// Handle writes the fields of one running unit of work and ends it. A Handle is not safe
// for concurrent use, because a unit of work is one sequence of events.
type Handle struct {
	ctx    context.Context
	log    *wlog.Logger
	kind   Kind
	group  string
	status StatusClass
	end    func()
	ended  bool

	// failures counts the failed items of a batch, which Failed folds into the parent
	// event.
	failures int
}

// Start opens one unit of work: it starts the event, reads the trace context from the
// carrier, writes the kind, the operation, and the group, and returns the handle that ends
// it. A nil Logger means wlog.Default.
func Start(ctx context.Context, log *wlog.Logger, u Unit) (context.Context, *Handle) {
	if log == nil {
		log = wlog.Default()
	}

	operation := u.Operation
	if operation == "" {
		operation = operationOf(u)
	}
	ctx, end := startEvent(ctx, log, operation)
	// The carrier is read after the event exists, so the ids of the incoming message
	// replace the trace ids the event started with.
	if u.Carrier != nil {
		ctx = propagate.Extract(ctx, u.Carrier)
	}

	h := &Handle{ctx: ctx, log: log, kind: u.Kind, group: groups[u.Kind], end: end}
	wlog.Set(ctx, "kind", string(u.Kind))
	if h.group != "" {
		wlog.SetGroup(ctx, h.group, u.Fields)
	}
	if lag := lagMS(u.StartedAt); lag > 0 {
		h.Set("lag_ms", lag)
	}
	return ctx, h
}

// startEvent opens the event of one unit of work. A unit that starts inside another one,
// which is what a message of a batch does, becomes its child: Detach keeps the trace of
// the parent and records trace.parent_event_id, so a reader walks from the batch to its
// messages.
func startEvent(ctx context.Context, log *wlog.Logger, operation string) (context.Context, func()) {
	if wlog.HasEvent(ctx) {
		return wlog.Detach(log.WithContext(ctx), operation)
	}
	ctx, end := wlog.Start(log.WithContext(ctx), operation)
	return ctx, end
}

// Set writes one field into the group of the kind. A kind with no group, which is kind
// work, has nowhere to put it, so this does nothing.
func (h *Handle) Set(key string, value any) {
	if h.group == "" {
		return
	}
	wlog.SetGroup(h.ctx, h.group, key, value)
}

// Status records the status of the unit in the group of its kind, as status_code, or as
// exit_code for a command, plus the class that decides the level at End.
func (h *Handle) Status(code string, class StatusClass) {
	h.status = class
	if h.kind == KindCommand {
		h.Set("exit_code", code)
		return
	}
	h.Set("status_code", code)
}

// End records the error, picks the level and the outcome, and emits the event once. A
// second End does nothing.
//
// The level follows the rule of SPEC.md: an error gives error, and two cases give warn
// instead, which are an ErrorInfo status from 400 to 499 and a client status class. With
// no error the status class decides, and otherwise the level stays as it is.
func (h *Handle) End(err error) {
	if h.ended {
		return
	}
	h.ended = true

	if err != nil {
		wlog.Error(h.ctx, err)
	}
	if level := h.level(err); level != "" {
		wlog.SetLevel(h.ctx, level)
	}
	h.end()
}

// level returns the level that the error and the status class ask for, and an empty level
// when neither asks for one.
func (h *Handle) level(err error) wlog.Level {
	switch {
	case err != nil && (h.status == StatusClientError || clientStatus(h.ctx)):
		return wlog.LevelWarn
	case err != nil:
		return wlog.LevelError
	case h.status == StatusClientError:
		return wlog.LevelWarn
	case h.status == StatusServerError:
		return wlog.LevelError
	}
	return ""
}

// clientStatus reports whether the error recorded on this unit carries a status from 400
// to 499, which is the caller's fault.
func clientStatus(ctx context.Context) bool {
	info, ok := wlog.CurrentError(ctx)
	return ok && info.Status >= 400 && info.Status <= 499
}

// operationOf builds the operation of one kind from its fields, by the table of
// SPEC-work.
func operationOf(u Unit) string {
	field := func(key string) string {
		text, _ := u.Fields[key].(string)
		return text
	}
	switch u.Kind {
	case KindRPC:
		return strings.Trim(field("service")+"/"+field("method"), "/")
	case KindMessage:
		return strings.Trim(field("operation")+" "+field("destination"), " ")
	case KindJob:
		return strings.Trim("job "+field("name"), " ")
	case KindCommand:
		return field("path")
	case KindFunction:
		return strings.Trim("function "+field("name"), " ")
	}
	return ""
}

// lagMS returns the milliseconds between the start of the work and now, and 0 for a zero
// time, which means the work started now.
func lagMS(startedAt time.Time) float64 {
	if startedAt.IsZero() {
		return 0
	}
	lag := time.Since(startedAt)
	if lag <= 0 {
		return 0
	}
	return float64(lag.Microseconds()) / 1000
}

// RunOption configures one Run call.
type RunOption func(*runConfig)

// runConfig holds what a caller asked Run to do.
type runConfig struct {
	recoverPanics bool
	flush         bool
}

// RecoverPanics returns a panic from the handler as an error instead of panicking again,
// which is what a consumer of a queue wants: one bad message must not end the process.
func RecoverPanics() RunOption { return func(c *runConfig) { c.recoverPanics = true } }

// Run opens one unit of work, runs fn inside it, and ends the event with what fn returned.
//
// A panic in fn is recorded with its stack, the event emits, and the panic continues by
// default. RecoverPanics returns it as an error instead.
func Run(ctx context.Context, log *wlog.Logger, u Unit, fn func(context.Context) error, opts ...RunOption) error {
	if log == nil {
		log = wlog.Default()
	}
	cfg := runConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx, h := Start(ctx, log, u)
	recovered, err := callSafe(ctx, fn)
	h.End(err)
	if cfg.flush {
		_ = log.Flush(ctx)
	}

	if recovered != nil && !cfg.recoverPanics {
		// The event is out, so the caller sees the panic it raised, value and all.
		panic(recovered)
	}
	return err
}

// callSafe runs fn under recover. It returns the recovered value of a panic, which is nil
// when fn returned, and the error of fn, which carries the stack of a panic.
func callSafe(ctx context.Context, fn func(context.Context) error) (recovered any, err error) {
	defer func() {
		if r := recover(); r != nil {
			recovered = r
			err = &panicError{value: r, stack: string(debug.Stack())}
		}
	}()
	return nil, fn(ctx)
}

// panicError carries a recovered panic value and the stack of the moment it was recovered.
// Its Stack method is read by the default ErrorExtractor of core.
type panicError struct {
	value any
	stack string
}

// Error returns the panic value as a message.
func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.value) }

// Stack returns the stack of the panic.
func (e *panicError) Stack() string { return e.stack }
