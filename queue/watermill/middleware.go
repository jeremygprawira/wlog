// This file holds the handler middleware, the mapping from one handled message onto one unit
// of work, and the reader of the subscriber type name.
package wlogwatermill

import (
	"context"
	"strings"

	"github.com/ThreeDotsLabs/watermill/message"
	watermillmiddleware "github.com/ThreeDotsLabs/watermill/message/router/middleware"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// resultDeadLetter is the result of a message that the PoisonQueue middleware moved to the
// poison topic.
const resultDeadLetter = "dead_letter"

// Middleware returns the handler middleware that gives every handled message one event. Add
// it before the other router middleware, so it is outermost and sees what they decided. A
// message that the PoisonQueue middleware moved to the poison topic records result
// dead_letter and level error. A nil Logger means wlog.Default.
func Middleware(log *wlog.Logger) message.HandlerMiddleware {
	return func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			// The reason is read before the handler. A requeued message keeps the metadata
			// of its first delivery, so a later success would look poisoned.
			before := msg.Metadata.Get(watermillmiddleware.ReasonForPoisonedKey)
			var produced []*message.Message
			err := work.Run(msg.Context(), log, unitOf(msg), func(ctx context.Context) error {
				// The handler reads the context of the message, so it carries the event.
				msg.SetContext(ctx)
				var handlerErr error
				produced, handlerErr = h(msg)
				if handlerErr == nil && before == "" {
					if reason := msg.Metadata.Get(watermillmiddleware.ReasonForPoisonedKey); reason != "" {
						// The PoisonQueue middleware published the message and returned nil,
						// so the record names the state the library chose.
						wlog.SetGroup(ctx, "messaging", "result", resultDeadLetter)
						wlog.SetLevel(ctx, wlog.LevelError)
					}
				}
				return handlerErr
			})
			return produced, err
		}
	}
}

// unitOf maps one handled message onto a unit of work. The router puts the handler name, the
// subscriber type, and the topics on the message context, so the event names them. The
// handler name goes under messaging.watermill, because the work table has no field for it.
func unitOf(msg *message.Message) work.Unit {
	ctx := msg.Context()
	fields := map[string]any{
		"system":    systemOf(message.SubscriberNameFromCtx(ctx)),
		"operation": "process",
	}
	if topic := message.SubscribeTopicFromCtx(ctx); topic != "" {
		fields["destination"] = topic
	}
	if msg.UUID != "" {
		fields["message_id"] = msg.UUID
	}
	if handler := message.HandlerNameFromCtx(ctx); handler != "" {
		fields["watermill"] = map[string]any{"handler": handler}
	}
	return work.Unit{
		Kind:    work.KindMessage,
		Fields:  fields,
		Carrier: propagate.MapCarrier(msg.Metadata),
	}
}

// systems maps the package of a subscriber type to the messaging system of the event. The
// table names the pubsubs of Watermill, and every other subscriber reports watermill.
var systems = map[string]string{
	"kafka":       "kafka",
	"amqp":        "rabbitmq",
	"nats":        "nats",
	"googlecloud": "gcp_pubsub",
	"aws":         "aws_sqs",
}

// systemOf returns the messaging system of one subscriber type name, such as
// "gochannel.GoChannel". A name with no entry in the table reports watermill.
func systemOf(name string) string {
	short := strings.TrimPrefix(name, "*")
	if dot := strings.IndexByte(short, '.'); dot >= 0 {
		short = short[:dot]
	}
	if system, ok := systems[strings.ToLower(short)]; ok {
		return system
	}
	return "watermill"
}
