//go:build cgo

// This file holds the consumer side: the loop around ReadMessage and the mapping from one
// message onto one unit of work.
package wlogconfluent

import (
	"context"
	"errors"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// readTimeout is how long one ReadMessage call waits for a message before the loop checks the
// context again.
const readTimeout = 100 * time.Millisecond

// Consumer is the part of *kafka.Consumer that Consume uses. *kafka.Consumer satisfies it,
// and a test fake does too, because no test can build a real consumer without a broker.
type Consumer interface {
	// ReadMessage waits up to timeout for one message.
	ReadMessage(timeout time.Duration) (*kafka.Message, error)
	// CommitMessage marks one message as processed.
	CommitMessage(msg *kafka.Message) ([]kafka.TopicPartition, error)
}

// The real consumer satisfies the contract of this adapter, so a confluent change that breaks
// it fails the build and not a user.
var _ Consumer = (*kafka.Consumer)(nil)

// Handler handles one message inside its event.
type Handler func(ctx context.Context, msg *kafka.Message) error

// Consume reads messages until the context ends, a read fails for a reason other than a
// timeout, or a handler fails. Each message gets one event, and Consume commits the message
// after the handler returns nil. A failed handler is not committed, and the loop stops,
// because a commit of a later offset covers the failed message. Start a new consumer, or seek
// back, to retry it. Set enable.auto.commit=false, because librdkafka commits on its own by
// default and would cover the failed message too. A nil Logger means wlog.Default.
func Consume(ctx context.Context, log *wlog.Logger, c Consumer, fn Handler) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msg, err := c.ReadMessage(readTimeout)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			return err
		}
		err = work.Run(ctx, log, unitOf(msg), func(ctx context.Context) error {
			return fn(ctx, msg)
		})
		if err != nil {
			return err
		}
		if _, err := c.CommitMessage(msg); err != nil {
			return err
		}
	}
}

// isTimeout reports whether a read error is the empty poll of a timeout.
func isTimeout(err error) bool {
	var kafkaErr kafka.Error
	return errors.As(err, &kafkaErr) && kafkaErr.IsTimeout()
}

// unitOf maps one message onto a unit of work. The message time becomes the start time, so
// the event carries the time the message waited as lag_ms.
func unitOf(msg *kafka.Message) work.Unit {
	fields := map[string]any{
		"system":    "kafka",
		"operation": "process",
		"partition": int(msg.TopicPartition.Partition),
		"offset":    int64(msg.TopicPartition.Offset),
	}
	if topic := topicOf(msg); topic != "" {
		fields["destination"] = topic
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   carrierOf(msg.Headers),
		StartedAt: msg.Timestamp,
	}
}

// topicOf returns the topic of one message, and an empty string when the message carries
// none.
func topicOf(msg *kafka.Message) string {
	if msg.TopicPartition.Topic == nil {
		return ""
	}
	return *msg.TopicPartition.Topic
}

// carrierOf wraps the headers of one message as a propagate carrier, so a traceparent header
// joins the trace of the producer.
func carrierOf(headers []kafka.Header) propagate.Carrier {
	values := make(map[string][]byte, len(headers))
	for _, header := range headers {
		values[header.Key] = header.Value
	}
	return propagate.NewBytesCarrier(values)
}
