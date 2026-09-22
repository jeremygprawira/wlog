// This file holds the observability service, the recover wrapper, and the mapping from one
// CloudEvent onto one unit of work.
package wlogcloudevents

import (
	"context"
	"fmt"
	"runtime/debug"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/cloudevents/sdk-go/v2/client"
	"github.com/cloudevents/sdk-go/v2/protocol"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Observability returns the client.ObservabilityService that gives every received event one
// event of work, and every sent event one call. Install it with
// client.WithObservabilityService.
//
// The SDK does not call the callback of a function that panics, so wrap the function with
// Recover to close the event of a panic.
func Observability(log *wlog.Logger) client.ObservabilityService {
	return observability{log: log}
}

// observability records the receive and the send side of one client.
type observability struct {
	log *wlog.Logger
}

// InboundContextDecorators returns no decorator, because the trace of a received event is read
// from its extensions in RecordCallingInvoker.
func (observability) InboundContextDecorators() []func(context.Context, binding.Message) context.Context {
	return nil
}

// RecordReceivedMalformedEvent records nothing, because a malformed event carries no unit of
// work. The SDK reports it through its own logger.
func (observability) RecordReceivedMalformedEvent(context.Context, error) {}

// RecordCallingInvoker starts one event for one received event, and returns the end func that
// the SDK calls with the result of the function. An acknowledgement is a non-nil result, so
// the end func maps it to no error.
func (o observability) RecordCallingInvoker(ctx context.Context, event *cloudevents.Event) (context.Context, func(error)) {
	if event == nil {
		return ctx, func(error) {}
	}
	ctx, handle := work.Start(ctx, o.log, unitOf(*event))
	return ctx, func(result error) { handle.End(ackError(result)) }
}

// ackError returns the error the event records for one receive result. An acknowledgement,
// and a nil result, record no error.
func ackError(result error) error {
	if protocol.IsACK(result) {
		return nil
	}
	return result
}

// RecordSendingEvent starts one call for one sent event, and returns the end func that the SDK
// calls with the result. The trace headers are written here, because the SDK runs the
// defaulters before this hook and the span id must be the span id of the call.
func (observability) RecordSendingEvent(ctx context.Context, event cloudevents.Event) (context.Context, func(error)) {
	ctx, end := wlog.StartCall(ctx, callOf(event))
	injectTrace(ctx, event)
	return ctx, func(result error) { end(resultOf(result)) }
}

// RecordRequestEvent starts one call for one requested event, and returns the end func that the
// SDK calls with the result. The trace headers are written here, for the same reason as the
// send side.
func (observability) RecordRequestEvent(ctx context.Context, event cloudevents.Event) (context.Context, func(error, *cloudevents.Event)) {
	ctx, end := wlog.StartCall(ctx, callOf(event))
	injectTrace(ctx, event)
	return ctx, func(result error, _ *cloudevents.Event) { end(resultOf(result)) }
}

// Recover wraps one CloudEvents function so a panic becomes an error with a stack. The SDK skips
// the observability callback of a function that panics, so the wrapper recovers the panic first
// and the event still ends.
func Recover(fn func(context.Context, cloudevents.Event) error) func(context.Context, cloudevents.Event) error {
	return func(ctx context.Context, event cloudevents.Event) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = &panicError{value: recovered, stack: string(debug.Stack())}
			}
		}()
		return fn(ctx, event)
	}
}

// panicError carries a recovered panic value and the stack of the moment it was recovered. Its
// Stack method is read by the default ErrorExtractor of core.
type panicError struct {
	value any
	stack string
}

// Error returns the panic value as a message.
func (e *panicError) Error() string { return fmt.Sprintf("panic: %v", e.value) }

// Stack returns the stack of the panic.
func (e *panicError) Stack() string { return e.stack }

// Unit maps one CloudEvent onto a unit of work, with the field set of this module. A receiver
// that does not drive the CloudEvents client, such as the GCF adapter, calls Unit so its events
// carry the same fields.
func Unit(event cloudevents.Event) work.Unit { return unitOf(event) }

// unitOf maps one received event onto a unit of work. The event time becomes the start time, so
// the event carries the time the message waited as lag_ms. The CloudEvents ids go under
// messaging.cloudevents, and the subject is the destination of the operation.
func unitOf(event cloudevents.Event) work.Unit {
	fields := map[string]any{
		"system":    "cloudevents",
		"operation": "process",
	}
	if event.Subject() != "" {
		fields["destination"] = event.Subject()
	}
	cloudeventsFields := map[string]any{}
	if event.ID() != "" {
		cloudeventsFields["event_id"] = event.ID()
	}
	if event.Source() != "" {
		cloudeventsFields["event_source"] = event.Source()
	}
	if event.Type() != "" {
		cloudeventsFields["event_type"] = event.Type()
	}
	if event.Subject() != "" {
		cloudeventsFields["event_subject"] = event.Subject()
	}
	if len(cloudeventsFields) > 0 {
		fields["cloudevents"] = cloudeventsFields
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   extensionCarrier{event: event},
		StartedAt: event.Time(),
	}
}

// extensionCarrier adapts the extensions of one CloudEvent to the propagate.Carrier interface.
type extensionCarrier struct {
	event cloudevents.Event
}

// Get returns one extension as text, and an empty string when the extension is absent.
func (c extensionCarrier) Get(key string) string {
	value, err := c.event.Context.GetExtension(key)
	if err != nil || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// Set stores one extension.
func (c extensionCarrier) Set(key, value string) { c.event.SetExtension(key, value) }

// Keys returns every extension name of the event.
func (c extensionCarrier) Keys() []string {
	keys := make([]string, 0, len(c.event.Extensions()))
	for key := range c.event.Extensions() {
		keys = append(keys, key)
	}
	return keys
}

// callOf names the call of one sent event: a queue publish to the subject of the event.
func callOf(event cloudevents.Event) wlog.Call {
	return wlog.Call{Kind: "queue", System: "cloudevents", Operation: "publish", Target: event.Subject()}
}

// resultOf builds the call result of one finished send, from the CloudEvents result. An
// acknowledgement, and a nil result, report success.
func resultOf(result error) wlog.CallResult {
	if protocol.IsACK(result) {
		return wlog.CallResult{Status: "ack"}
	}
	return wlog.CallResult{Err: result}
}

// injectTrace writes the trace headers of ctx as extensions of one event, when ctx carries a
// trace. A CloudEvents extension name holds lowercase letters, digits, and hyphens, so the
// request id of the trace stays out.
func injectTrace(ctx context.Context, event cloudevents.Event) {
	if _, ok := propagate.FromContext(ctx); !ok {
		return
	}
	carrier := propagate.MapCarrier{}
	propagate.Inject(ctx, carrier)
	for _, key := range []string{"traceparent", "tracestate"} {
		if value := carrier.Get(key); value != "" {
			event.SetExtension(key, value)
		}
	}
}

// EventDefaulter returns the defaulter that writes the trace headers of the context as
// extensions, so a caller that sends an event outside the observability service joins the
// same trace. Install it with client.WithEventDefaulter.
func EventDefaulter() client.EventDefaulter {
	return func(ctx context.Context, event cloudevents.Event) cloudevents.Event {
		injectTrace(ctx, event)
		return event
	}
}
