// This file holds the consumer side: the handler that owns a claim loop, the helper for a
// user loop, and the mapping from one sarama message onto one unit of work.
package wlogsarama

import (
	"context"

	"github.com/IBM/sarama"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// MessageHandler handles one message inside its event.
type MessageHandler func(ctx context.Context, msg *sarama.ConsumerMessage) error

// Handler returns a sarama.ConsumerGroupHandler that gives every claimed message one event.
// It owns the claim loop: it marks a message with session.MarkMessage after the handler
// returns nil, and it leaves a failed message unmarked, so the group delivers it again.
//
// A panic in the handler becomes an error with a stack, and the loop continues, so one bad
// message does not end the claim.
func Handler(log *wlog.Logger, fn MessageHandler) sarama.ConsumerGroupHandler {
	return groupHandler{log: log, fn: fn}
}

// groupHandler runs one event per claimed message.
type groupHandler struct {
	log *wlog.Logger
	fn  MessageHandler
}

// Setup runs before the claims of one generation, and records nothing.
func (groupHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

// Cleanup runs after the claims of one generation, and records nothing.
func (groupHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim reads the messages of one claim until the session ends or the claim closes.
func (h groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-session.Context().Done():
			return nil
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			err := process(session.Context(), h.log, unitOf(session, claim, msg), func(ctx context.Context) error {
				return h.fn(ctx, msg)
			})
			if err != nil {
				continue
			}
			session.MarkMessage(msg, "")
		}
	}
}

// Message starts one event for one message of a user loop, and returns the context of the
// handler and the end func. Pass the claim that owns the message, so the event carries the
// offset lag. Mark the message after the end func reports success.
func Message(log *wlog.Logger, session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim, msg *sarama.ConsumerMessage) (context.Context, func(error)) {
	ctx, h := work.Start(session.Context(), log, unitOf(session, claim, msg))
	return ctx, h.End
}

// process runs one unit of work through the event path of this adapter: one event, the group
// of the kind, and a recovered panic as an error.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// unitOf maps one sarama message onto a unit of work. The message timestamp becomes the
// start time, so the event carries the time the message waited as lag_ms. The member id and
// the generation of the session go under messaging.kafka.
func unitOf(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim, msg *sarama.ConsumerMessage) work.Unit {
	kafkaFields := map[string]any{
		"member_id":  session.MemberID(),
		"generation": session.GenerationID(),
	}
	if lag := claim.HighWaterMarkOffset() - msg.Offset - 1; lag >= 0 {
		kafkaFields["offset_lag"] = lag
	}
	return work.Unit{
		Kind: work.KindMessage,
		Fields: map[string]any{
			"system":      "kafka",
			"operation":   "process",
			"destination": msg.Topic,
			"partition":   msg.Partition,
			"offset":      msg.Offset,
			"kafka":       kafkaFields,
		},
		Carrier:   carrierOf(msg.Headers),
		StartedAt: msg.Timestamp,
	}
}

// carrierOf wraps the headers of one message as a propagate carrier, so a traceparent
// header joins the trace of the producer.
func carrierOf(headers []*sarama.RecordHeader) propagate.Carrier {
	values := make(map[string][]byte, len(headers))
	for _, header := range headers {
		if header != nil {
			values[string(header.Key)] = header.Value
		}
	}
	return propagate.NewBytesCarrier(values)
}
