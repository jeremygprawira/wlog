// This file holds the consumer side: the reader contract, the handler it runs, and the
// mapping from one Kafka message onto one unit of work.
package wlogkafkago

import (
	"context"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Fetcher is the part of *kafka.Reader that Consume uses. *kafka.Reader satisfies it, and
// a test fake satisfies it too, because no test can build a real Reader without a broker.
type Fetcher interface {
	// FetchMessage reads the next message and leaves it uncommitted.
	FetchMessage(ctx context.Context) (kafka.Message, error)
	// CommitMessages marks the messages as processed.
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	// Config returns the reader configuration, which carries the consumer group.
	Config() kafka.ReaderConfig
}

// The real reader satisfies the contract of the adapter, so a kafka-go change that breaks
// it fails the build and not a user.
var _ Fetcher = (*kafka.Reader)(nil)

// Handler handles one message inside its event.
type Handler func(ctx context.Context, msg kafka.Message) error

// Consume fetches one message, runs fn inside one event, and commits the message after fn
// returns nil. A failed handler is not committed, and Consume returns the handler error, so
// the caller decides to retry or to skip. Call Consume in a loop. A nil Logger means
// wlog.Default.
//
// A kafka.Reader moves its read position on every fetch, commit or not. A caller that keeps
// the same reader after an error commits past the failed message. To retry it, stop the loop
// and open a new reader, or seek the reader back to the failed offset.
//
// Consume never uses ReadMessage, because ReadMessage commits before the handler runs.
func Consume(ctx context.Context, log *wlog.Logger, r Fetcher, fn Handler) error {
	msg, err := r.FetchMessage(ctx)
	if err != nil {
		return err
	}
	err = work.Run(ctx, log, unitOf(msg, r.Config().GroupID), func(ctx context.Context) error {
		return fn(ctx, msg)
	})
	if err != nil {
		return err
	}
	return r.CommitMessages(ctx, msg)
}

// unitOf maps one Kafka message onto a unit of work. The message time becomes the start
// time, so the event carries the time the message waited as lag_ms. The headers become the
// carrier, so a traceparent header joins the trace of the producer.
func unitOf(msg kafka.Message, group string) work.Unit {
	fields := map[string]any{
		"system":      "kafka",
		"operation":   "process",
		"destination": msg.Topic,
		"partition":   msg.Partition,
		"offset":      msg.Offset,
	}
	if group != "" {
		fields["consumer_group"] = group
	}
	// The offset lag counts the messages behind the last one. A reader with no group
	// reports no high water mark, and a negative count means the same.
	if lag := msg.HighWaterMark - msg.Offset - 1; lag >= 0 {
		fields["kafka"] = map[string]any{"offset_lag": lag}
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   carrierOf(msg.Headers),
		StartedAt: msg.Time,
	}
}

// carrierOf wraps the headers of one message as a propagate carrier, so a traceparent
// header joins the trace of the producer.
func carrierOf(headers []kafka.Header) propagate.Carrier {
	values := make(map[string][]byte, len(headers))
	for _, header := range headers {
		values[header.Key] = header.Value
	}
	return propagate.NewBytesCarrier(values)
}
