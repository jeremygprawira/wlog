// This file holds the publisher decorator: one call per publish, and the trace metadata of
// the message context.
package wlogwatermill

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// PublisherDecorator returns the decorator that records one call per publish and writes the
// trace headers of the message context into the metadata of each message.
func PublisherDecorator() message.PublisherDecorator {
	return func(p message.Publisher) (message.Publisher, error) {
		return publisher{p: p}, nil
	}
}

// publisher records one call around the Publish call of the wrapped publisher.
type publisher struct {
	p message.Publisher
}

// Publish writes the trace metadata of each message, calls the wrapped publisher, and records
// one call on the event of the message context. The call names the first message context,
// because one publish call carries one context.
func (p publisher) Publish(topic string, messages ...*message.Message) error {
	ctx := context.Background()
	if len(messages) > 0 {
		ctx = messages[0].Context()
	}
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "watermill", Operation: "publish", Target: topic,
	})
	for _, msg := range messages {
		withTraceMetadata(ctx, msg)
	}
	err := p.p.Publish(topic, messages...)
	end(resultOf(err))
	return err
}

// Close closes the wrapped publisher.
func (p publisher) Close() error { return p.p.Close() }

// withTraceMetadata writes the trace context of ctx into the metadata of one message, so the
// next service joins the same trace. A context with no trace context keeps the metadata as it
// was.
func withTraceMetadata(ctx context.Context, msg *message.Message) {
	if _, ok := propagate.FromContext(ctx); !ok {
		return
	}
	propagate.Inject(ctx, propagate.MapCarrier(msg.Metadata))
}

// resultOf builds the call result of one finished publish.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
