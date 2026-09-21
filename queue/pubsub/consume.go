// This file holds the consumer side: the receive loop, the ack rule, and the mapping from one
// message onto one unit of work.
package wlogpubsub

import (
	"context"

	"cloud.google.com/go/pubsub/v2"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// Receiver is the part of *pubsub.Subscriber that Receive uses. *pubsub.Subscriber satisfies it,
// and a test fake does too, because no test can reach the service.
type Receiver interface {
	Receive(ctx context.Context, f func(context.Context, *pubsub.Message)) error
	ID() string
}

// Handler handles one message inside its event.
type Handler func(ctx context.Context, msg *pubsub.Message) error

// Receive reads messages through sub until the context ends. Each message gets one event, and
// Receive acks the message after the handler returns nil and nacks it after an error. The
// library runs the callback from several goroutines, so the events of a batch overlap. A nil
// Logger means wlog.Default.
func Receive(ctx context.Context, log *wlog.Logger, sub Receiver, fn Handler) error {
	return sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		err := work.Run(ctx, log, unitOf(msg, sub.ID()), func(ctx context.Context) error {
			return fn(ctx, msg)
		})
		if err != nil {
			msg.Nack()
			return
		}
		msg.Ack()
	})
}

// unitOf maps one message onto a unit of work. The publish time becomes the start time, so the
// event carries the time the message waited as lag_ms.
func unitOf(msg *pubsub.Message, subscription string) work.Unit {
	fields := map[string]any{
		"system":    "gcp_pubsub",
		"operation": "process",
	}
	if subscription != "" {
		fields["destination"] = subscription
	}
	if msg.ID != "" {
		fields["message_id"] = msg.ID
	}
	if msg.DeliveryAttempt != nil {
		fields["delivery_count"] = *msg.DeliveryAttempt
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   attributesCarrier(msg.Attributes),
		StartedAt: msg.PublishTime,
	}
}

// attributesCarrier adapts the attributes of one message to the propagate.Carrier interface. A
// missing plain key reads the key with the googclient prefix, which is the name the Pub/Sub
// client gives its own trace headers.
type attributesCarrier map[string]string

// Get returns the value of key, and of the googclient prefixed key when the plain one is absent.
func (c attributesCarrier) Get(key string) string {
	if value, ok := c[key]; ok {
		return value
	}
	return c["googclient_"+key]
}

// Set stores value under key.
func (c attributesCarrier) Set(key, value string) { c[key] = value }

// Keys returns every key of the attributes.
func (c attributesCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}
