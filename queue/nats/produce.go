// This file holds the producer side: one call per publish, and the trace headers of the
// context.
package wlognats

import (
	"context"

	"github.com/nats-io/nats.go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Publisher is the part of *nats.Conn that this adapter uses. *nats.Conn satisfies it, and a
// test fake does too, because no test can reach a server.
type Publisher interface {
	PublishMsg(msg *nats.Msg) error
}

// Publish publishes one message on one subject, records one call on the event of ctx, and
// writes the trace headers of ctx into the message.
func Publish(ctx context.Context, p Publisher, subject string, data []byte) error {
	return PublishMsg(ctx, p, &nats.Msg{Subject: subject, Data: data})
}

// PublishMsg publishes one message, records one call on the event of ctx, and writes the trace
// headers of ctx into the message headers.
func PublishMsg(ctx context.Context, p Publisher, msg *nats.Msg) error {
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "nats", Operation: "publish", Target: msg.Subject,
	})
	out := *msg
	out.Header = withTraceHeaders(ctx, msg.Header)
	err := p.PublishMsg(&out)
	end(resultOf(err))
	return err
}

// withTraceHeaders returns a copy of the headers of one message with the trace context of ctx
// added. A context with no trace context keeps the headers as they were.
func withTraceHeaders(ctx context.Context, header nats.Header) nats.Header {
	if _, ok := propagate.FromContext(ctx); !ok {
		return header
	}
	out := make(nats.Header, len(header)+3)
	for key, values := range header {
		out[key] = append([]string(nil), values...)
	}
	propagate.Inject(ctx, headerCarrier(out))
	return out
}

// resultOf builds the call result of one finished publish.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
