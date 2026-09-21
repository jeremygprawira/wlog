//go:build cgo

// This file holds the producer side: the wrapper that records one call per produce, adds the
// trace headers of the context, and ends the call on the delivery report.
package wlogconfluent

import (
	"context"
	"errors"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Sender is the part of *kafka.Producer that Produce uses. *kafka.Producer satisfies it, and
// a test fake does too, because no test can build a real producer without a broker.
type Sender interface {
	// Produce sends one message and reports the delivery on deliveryChan.
	Produce(msg *kafka.Message, deliveryChan chan kafka.Event) error
}

// The real producer satisfies the contract of this adapter, so a confluent change that breaks
// it fails the build and not a user.
var _ Sender = (*kafka.Producer)(nil)

// errNoReport is the error of a delivery channel that closed before it reported.
var errNoReport = errors.New("wlogconfluent: the delivery channel closed with no report")

// Produce sends msg through p, records one call on the event of ctx, and adds the trace
// headers of ctx to msg. It waits for the delivery report, so the call covers the delivery,
// and it returns the error of that report. A caller that wants no wait runs Produce in its
// own goroutine.
func Produce(ctx context.Context, p Sender, msg *kafka.Message, deliveryChan chan kafka.Event) error {
	ctx, end := wlog.StartCall(ctx, callOf(msg))
	msg.Headers = withTraceHeaders(ctx, msg.Headers)

	// The report arrives on a private channel, so a nil caller channel and a shared channel
	// both work, and two calls never read each other's report.
	report := make(chan kafka.Event, 1)
	if err := p.Produce(msg, report); err != nil {
		end(wlog.CallResult{Err: err})
		return err
	}

	var event kafka.Event
	select {
	case received, ok := <-report:
		if !ok {
			end(wlog.CallResult{Err: errNoReport})
			return errNoReport
		}
		event = received
	case <-ctx.Done():
		end(wlog.CallResult{Err: ctx.Err()})
		return ctx.Err()
	}
	err := deliveryError(event)
	end(resultOf(err))

	// A caller that passed a channel still gets the report.
	if deliveryChan != nil {
		select {
		case deliveryChan <- event:
		case <-ctx.Done():
		}
	}
	return err
}

// deliveryError returns the error of one delivery event. A report with no error, and an event
// of another type, report no error.
func deliveryError(event kafka.Event) error {
	switch report := event.(type) {
	case *kafka.Message:
		if report != nil {
			return report.TopicPartition.Error
		}
	case kafka.Error:
		return report
	}
	return nil
}

// withTraceHeaders returns a copy of the headers of one message with the trace context of ctx
// added, so the next service joins the same trace. A context with no trace context keeps the
// headers exactly as they were, and a repeated header key keeps every value.
func withTraceHeaders(ctx context.Context, headers []kafka.Header) []kafka.Header {
	if _, ok := propagate.FromContext(ctx); !ok {
		return headers
	}
	carrier := propagate.NewBytesCarrier(nil)
	propagate.Inject(ctx, carrier)
	out := make([]kafka.Header, len(headers), len(headers)+3)
	copy(out, headers)
	for _, key := range carrier.Keys() {
		out = setHeader(out, key, carrier.Get(key))
	}
	return out
}

// setHeader returns headers with key set to value. It replaces the first header of that key
// and appends one when the key is absent, so every other header keeps its place.
func setHeader(headers []kafka.Header, key, value string) []kafka.Header {
	for i := range headers {
		if headers[i].Key == key {
			headers[i].Value = []byte(value)
			return headers
		}
	}
	return append(headers, kafka.Header{Key: key, Value: []byte(value)})
}

// callOf names the call of one produce: a queue publish to the topic of the message.
func callOf(msg *kafka.Message) wlog.Call {
	return wlog.Call{Kind: "queue", System: "kafka", Operation: "publish", Target: topicOf(msg)}
}

// resultOf builds the call result of one finished produce.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
