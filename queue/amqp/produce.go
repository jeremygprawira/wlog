// This file holds the producer side: one call per publish, and the trace headers of the context.
package wlogamqp

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Publisher is the part of *amqp.Channel that PublishWithContext uses. *amqp.Channel satisfies
// it, and a test fake does too, because no test can reach a broker.
type Publisher interface {
	PublishWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
}

// PublishWithContext publishes one message through channel, records one call on the event of
// ctx, and writes the trace headers of ctx into the message headers.
func PublishWithContext(ctx context.Context, channel Publisher, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "rabbitmq", Operation: "publish", Target: key,
	})
	msg.Headers = withTraceHeaders(ctx, msg.Headers)
	err := channel.PublishWithContext(ctx, exchange, key, mandatory, immediate, msg)
	end(resultOf(err))
	return err
}

// withTraceHeaders returns a copy of the headers of one message with the trace context of ctx
// added. A context with no trace context keeps the headers as they were.
func withTraceHeaders(ctx context.Context, headers amqp.Table) amqp.Table {
	if _, ok := propagate.FromContext(ctx); !ok {
		return headers
	}
	out := make(amqp.Table, len(headers)+3)
	for key, value := range headers {
		out[key] = value
	}
	propagate.Inject(ctx, tableCarrier(out))
	return out
}

// resultOf builds the call result of one finished publish.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
