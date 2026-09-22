// This file holds the producer side: one call per publish, and the trace attributes of the
// context.
package wlogpubsub

import (
	"context"

	"cloud.google.com/go/pubsub/v2"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Publisher is the part of *pubsub.Publisher that Publish uses. *pubsub.Publisher satisfies it,
// and a test fake does too.
type Publisher interface {
	Publish(ctx context.Context, msg *pubsub.Message) *pubsub.PublishResult
	ID() string
}

// Publish publishes one message, records one call on the event of ctx, and adds the trace
// headers of ctx to a copy of the message attributes. The call ends when the service reports the
// result of the publish, which the library reports asynchronously.
//
// ponytail: one goroutine per publish waits for the result. A shared result loop is the upgrade
// path if the publish rate matters.
func Publish(ctx context.Context, p Publisher, msg *pubsub.Message) *pubsub.PublishResult {
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "gcp_pubsub", Operation: "publish", Target: p.ID(),
	})
	msg.Attributes = withTraceAttributes(ctx, msg.Attributes)
	result := p.Publish(ctx, msg)
	go func() {
		// The event may end before the service answers, so the wait uses the publish
		// context and the result is read on a live one.
		<-result.Ready()
		_, err := result.Get(context.Background())
		end(resultOf(err))
	}()
	return result
}

// withTraceAttributes returns a copy of the attributes of one message with the trace headers of
// ctx added. A context with no trace context keeps the attributes as they were. The library
// shares an attribute map between messages, so the copy protects the caller.
func withTraceAttributes(ctx context.Context, attributes map[string]string) map[string]string {
	if _, ok := propagate.FromContext(ctx); !ok {
		return attributes
	}
	out := make(map[string]string, len(attributes)+3)
	for key, value := range attributes {
		out[key] = value
	}
	propagate.Inject(ctx, attributesCarrier(out))
	return out
}

// resultOf builds the call result of one finished publish.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
