// Package workconformance is the conformance suite for every adapter that turns a
// non-HTTP unit of work into one wlog event. A Kafka consumer, a gRPC server, a cron job,
// a command, and a function all pass the same scenarios, so "one shape" is a test result.
//
// The suite names a kind and a handler, and the adapter runs the unit through the work
// package. The adapter maps its own library onto the unit, so the suite imports no
// framework.
//
// A factory drives the real entry of its adapter with a fake client, so a change that breaks
// the entry fails the suite. Two adapters keep a shim, and the reason is recorded here:
// queue-franz returns an end func from its per-message helper and has no panic path, and
// queue-watermill puts the topic on a message context it exports no setter for.
package workconformance

import (
	"context"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/work"
)

// Factory runs one unit of work through one adapter, and returns the error the adapter
// reports. The factory drives the real entry of its adapter, and it recovers a panic of the
// handler and returns it as an error, so the suite continues. The entry itself records the
// panic and raises it again, as the track spec says.
type Factory interface {
	Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error
}

// Declaration describes one adapter to the suite, so a factory that cannot produce every kind
// still runs the scenarios that fit its adapter.
type Declaration struct {
	// Kinds lists the kinds the adapter produces. An empty list means every kind.
	Kinds []work.Kind
	// System is the messaging.system value the adapter writes. An empty value means kafka,
	// which the reference adapter writes.
	System string
	// DeliveryCount reports whether the library reports a redelivery count. Kafka and core
	// NATS report none.
	DeliveryCount bool
	// Attempt reports whether the factory can set the attempt of a job. A job adapter whose
	// library keeps the attempt in a context of its own reports false, and the suite skips
	// the attempt case.
	Attempt bool
}

// Declarer is the optional interface a factory implements to describe its adapter. The suite
// runs the Kinds scenario for the declared kinds only, and it builds the unit of every other
// scenario from the first declared kind.
type Declarer interface {
	Declare() Declaration
}

// declarationOf returns the declaration of one factory, and the reference declaration for a
// factory that declares nothing.
func declarationOf(factory Factory) Declaration {
	if declarer, ok := factory.(Declarer); ok {
		return declarer.Declare()
	}
	return Declaration{System: "kafka", DeliveryCount: true, Attempt: true}
}

// produces reports whether the adapter produces one kind.
func (d Declaration) produces(kind work.Kind) bool {
	if len(d.Kinds) == 0 {
		return true
	}
	for _, produced := range d.Kinds {
		if produced == kind {
			return true
		}
	}
	return false
}

// firstKind returns the first kind the adapter produces, and the message kind when it
// produces none.
func (d Declaration) firstKind() work.Kind {
	if len(d.Kinds) > 0 {
		return d.Kinds[0]
	}
	return work.KindMessage
}

// unitFor returns a unit of one kind with the fields that kind needs, so a scenario that only
// cares about the outcome still runs through the adapter's own mapping.
func unitFor(d Declaration, kind work.Kind) work.Unit {
	switch kind {
	case work.KindMessage:
		system := d.System
		if system == "" {
			system = "kafka"
		}
		return work.Unit{Kind: work.KindMessage, Fields: map[string]any{
			"system": system, "operation": "process", "destination": "orders",
		}}
	case work.KindJob:
		return work.Unit{Kind: work.KindJob, Fields: map[string]any{"name": "reindex"}}
	case work.KindRPC:
		return work.Unit{Kind: work.KindRPC, Fields: map[string]any{"service": "OrderService", "method": "Get"}}
	case work.KindCommand:
		return work.Unit{Kind: work.KindCommand, Fields: map[string]any{"path": "wlog map"}}
	case work.KindFunction:
		return work.Unit{Kind: work.KindFunction, Fields: map[string]any{"name": "handler"}}
	}
	return work.Unit{Kind: kind}
}

// Run runs every scenario against the adapter the factory builds.
func Run(t conformance.TB, factory Factory) {
	t.Helper()
	t.Run("Kinds", func(t conformance.TB) { testKinds(t, factory) })
	t.Run("HandlerError", func(t conformance.TB) { testHandlerError(t, factory) })
	t.Run("PanicStack", func(t conformance.TB) { testPanicStack(t, factory) })
	t.Run("DeliveryCountAndAttempt", func(t conformance.TB) { testDeliveryCountAndAttempt(t, factory) })
	t.Run("StarterAndFinisher", func(t conformance.TB) { testStarterAndFinisher(t, factory) })
}

// succeed is the handler of a unit that finishes without an error.
func succeed(context.Context) error { return nil }

// process runs one unit and returns the recorder plus the error the adapter reported.
func process(factory Factory, unit work.Unit, handler func(context.Context) error, opts ...wlog.Option) (*conformance.MemoryRecorder, error) {
	rec := conformance.NewMemoryRecorder()
	err := factory.Process(rec.Logger(opts...), unit, handler)
	return rec, err
}

// event returns the single event of one run, and reports a scenario that recorded none.
func event(t conformance.TB, name string, rec *conformance.MemoryRecorder) map[string]any {
	t.Helper()
	got := rec.Last()
	if got == nil {
		t.Errorf("%s: no event recorded", name)
	}
	return got
}

// group returns one group of an event, and reports a group that is missing.
func group(t conformance.TB, name string, got map[string]any, key string) map[string]any {
	t.Helper()
	fields, _ := got[key].(map[string]any)
	if fields == nil {
		t.Errorf("%s: the %s group is missing", name, key)
	}
	return fields
}

// testKinds proves that every kind the adapter produces writes its own group and the
// operation of its table.
func testKinds(t conformance.TB, factory Factory) {
	decl := declarationOf(factory)
	cases := []struct {
		name      string
		kind      work.Kind
		operation string
		group     string
		field     string
	}{
		{"message", work.KindMessage, "process orders", "messaging", "system"},
		{"job", work.KindJob, "job reindex", "job", "name"},
		{"rpc", work.KindRPC, "OrderService/Get", "rpc", "method"},
		{"command", work.KindCommand, "wlog map", "cli", "path"},
		{"function", work.KindFunction, "function handler", "faas", "name"},
	}

	for _, tc := range cases {
		if !decl.produces(tc.kind) {
			continue
		}
		name := "Kinds/" + tc.name
		unit := unitFor(decl, tc.kind)
		rec, err := process(factory, unit, succeed)
		if err != nil {
			t.Errorf("%s: Process returned %v", name, err)
		}
		got := event(t, name, rec)
		if got == nil {
			continue
		}
		if got["kind"] != string(unit.Kind) {
			t.Errorf("%s: kind = %v, want %s", name, got["kind"], unit.Kind)
		}
		if got["operation"] != tc.operation {
			t.Errorf("%s: operation = %v, want %s", name, got["operation"], tc.operation)
		}
		if got["level"] != "info" || got["outcome"] != "success" {
			t.Errorf("%s: level/outcome = %v/%v, want info/success", name, got["level"], got["outcome"])
		}
		if fields := group(t, name, got, tc.group); fields != nil && fields[tc.field] != unit.Fields[tc.field] {
			t.Errorf("%s: %s.%s = %v, want %v", name, tc.group, tc.field, fields[tc.field], unit.Fields[tc.field])
		}
	}
}

// testHandlerError proves that a handler error gives level and outcome error, records the
// error, and returns the same error to the caller.
func testHandlerError(t conformance.TB, factory Factory) {
	const name = "HandlerError"
	unit := unitFor(declarationOf(factory), declarationOf(factory).firstKind())
	failure := errString("reindex failed")

	rec, err := process(factory, unit, func(context.Context) error { return failure })
	if err == nil || err.Error() != "reindex failed" {
		t.Errorf("%s: Process returned %v, want the handler error", name, err)
	}
	got := event(t, name, rec)
	if got == nil {
		return
	}
	if got["level"] != "error" || got["outcome"] != "error" {
		t.Errorf("%s: level/outcome = %v/%v, want error/error", name, got["level"], got["outcome"])
	}
	info, _ := got["error"].(map[string]any)
	if info == nil || info["message"] != "reindex failed" {
		t.Errorf("%s: error.message = %v, want the handler error", name, got["error"])
	}
}

// testPanicStack proves that a panicking handler gives one error event with a stack.
func testPanicStack(t conformance.TB, factory Factory) {
	const name = "PanicStack"
	decl := declarationOf(factory)
	unit := unitFor(decl, decl.firstKind())

	rec, err := process(factory, unit, func(context.Context) error { panic("boom") })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("%s: Process returned %v, want the recovered panic", name, err)
	}
	got := event(t, name, rec)
	if got == nil {
		return
	}
	if got["level"] != "error" || got["outcome"] != "error" {
		t.Errorf("%s: level/outcome = %v/%v, want error/error", name, got["level"], got["outcome"])
	}
	info, _ := got["error"].(map[string]any)
	if info == nil || info["stack"] == "" || info["stack"] == nil {
		t.Errorf("%s: error.stack = %v, want the recovered stack", name, got["error"])
	}
}

// testDeliveryCountAndAttempt proves that a redelivered message and a retried job name
// their count in the group and in the summary.
func testDeliveryCountAndAttempt(t conformance.TB, factory Factory) {
	decl := declarationOf(factory)

	if decl.DeliveryCount {
		message := unitFor(decl, work.KindMessage)
		message.Fields["delivery_count"] = 3
		rec, _ := process(factory, message, succeed)
		if got := event(t, "DeliveryCountAndAttempt/message", rec); got != nil {
			if fields := group(t, "DeliveryCountAndAttempt/message", got, "messaging"); fields != nil && !conformance.Equal(fields["delivery_count"], 3) {
				t.Errorf("message: messaging.delivery_count = %v, want 3", fields["delivery_count"])
			}
			if summary, _ := got["summary"].(string); !strings.Contains(summary, "delivery 3") {
				t.Errorf("message: summary = %q, want it to name delivery 3", summary)
			}
		}
	}

	if decl.Attempt && decl.produces(work.KindJob) {
		job := unitFor(decl, work.KindJob)
		job.Fields["attempt"] = 2
		rec, _ := process(factory, job, succeed)
		if got := event(t, "DeliveryCountAndAttempt/job", rec); got != nil {
			if fields := group(t, "DeliveryCountAndAttempt/job", got, "job"); fields != nil && !conformance.Equal(fields["attempt"], 2) {
				t.Errorf("job: job.attempt = %v, want 2", fields["attempt"])
			}
			if summary, _ := got["summary"].(string); !strings.Contains(summary, "attempt 2") {
				t.Errorf("job: summary = %q, want it to name attempt 2", summary)
			}
		}
	}
}

// testStarterAndFinisher proves that a plugin's Starter and Finisher hooks each run once
// for one unit of work.
func testStarterAndFinisher(t conformance.TB, factory Factory) {
	const name = "StarterAndFinisher"
	plugin := &counterPlugin{}
	decl := declarationOf(factory)
	unit := unitFor(decl, decl.firstKind())

	_, _ = process(factory, unit, succeed, wlog.WithPlugins(plugin))

	if plugin.starts != 1 {
		t.Errorf("%s: Starter ran %d times, want 1", name, plugin.starts)
	}
	if plugin.finishes != 1 {
		t.Errorf("%s: Finisher ran %d times, want 1", name, plugin.finishes)
	}
}

// counterPlugin counts the Starter and Finisher calls of one unit of work.
type counterPlugin struct {
	starts   int
	finishes int
}

// Name satisfies the Plugin contract.
func (*counterPlugin) Name() string { return "counter" }

// OnStart counts one start.
func (p *counterPlugin) OnStart(ctx context.Context, _ string) context.Context {
	p.starts++
	return ctx
}

// OnFinish counts one finish.
func (p *counterPlugin) OnFinish(context.Context, wlog.Event) { p.finishes++ }

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
