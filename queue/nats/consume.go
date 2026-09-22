// This file holds the consumer side: the core and JetStream handlers, the ack rule of
// JetStream, and the mapping from one message onto one unit of work.
package wlognats

import (
	"context"
	"errors"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// MessageHandler handles one core NATS message inside its event.
type MessageHandler func(ctx context.Context, msg *nats.Msg) error

// Handler returns the core NATS message handler that gives every message one event. Core NATS
// has no acknowledgement, so a failed handler records the error and returns.
func Handler(log *wlog.Logger, fn MessageHandler) nats.MsgHandler {
	return func(msg *nats.Msg) {
		_ = work.Run(context.Background(), log, unitOf(msg), func(ctx context.Context) error {
			return fn(ctx, msg)
		})
	}
}

// JetStreamHandlerFunc handles one JetStream message inside its event.
type JetStreamHandlerFunc func(ctx context.Context, msg jetstream.Msg) error

// Option configures the JetStream handler.
type Option func(*config)

// config holds the resolved options of one JetStream handler.
type config struct {
	termOn []error
}

// TermOn names the errors that term a message instead of nacking it. A term stops the stream
// from delivering the message again, which suits an error that no retry can fix.
func TermOn(errs ...error) Option {
	return func(c *config) { c.termOn = append(c.termOn, errs...) }
}

// JetStreamHandler returns the JetStream message handler that gives every message one event. It
// acks the message after the handler returns nil, terms it when the error matches a TermOn
// error, and nacks it otherwise so the stream delivers it again. A nil Logger means
// wlog.Default.
//
// The settle runs after the event ends, so an error of the ack, the term, or the nack reaches
// the stream and not the event. The stream reports the state of the message.
func JetStreamHandler(log *wlog.Logger, fn JetStreamHandlerFunc, opts ...Option) jetstream.MessageHandler {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(msg jetstream.Msg) {
		err := work.Run(context.Background(), log, jetStreamUnit(msg), func(ctx context.Context) error {
			return fn(ctx, msg)
		})
		switch {
		case err == nil:
			_ = msg.Ack()
		case matches(err, cfg.termOn):
			_ = msg.Term()
		default:
			_ = msg.Nak()
		}
	}
}

// matches reports whether err matches one of the errors.
func matches(err error, errs []error) bool {
	for _, target := range errs {
		if target != nil && errors.Is(err, target) {
			return true
		}
	}
	return false
}

// unitOf maps one core NATS message onto a unit of work. The subscription names the queue group
// and the pattern it matched.
func unitOf(msg *nats.Msg) work.Unit {
	fields := map[string]any{
		"system":      "nats",
		"operation":   "process",
		"destination": msg.Subject,
	}
	if msg.Sub != nil {
		if msg.Sub.Queue != "" {
			fields["consumer_group"] = msg.Sub.Queue
		}
		if msg.Sub.Subject != "" && msg.Sub.Subject != msg.Subject {
			fields["nats"] = map[string]any{"subscription": msg.Sub.Subject}
		}
	}
	return work.Unit{
		Kind:    work.KindMessage,
		Fields:  fields,
		Carrier: headerCarrier(msg.Header),
	}
}

// jetStreamUnit maps one JetStream message onto a unit of work. The metadata names the stream,
// the consumer, and the sequence, and its time becomes the start time, so the event carries the
// time the message waited as lag_ms.
func jetStreamUnit(msg jetstream.Msg) work.Unit {
	u := work.Unit{
		Kind: work.KindMessage,
		Fields: map[string]any{
			"system":      "nats",
			"operation":   "process",
			"destination": msg.Subject(),
		},
		Carrier: headerCarrier(msg.Headers()),
	}
	metadata, err := msg.Metadata()
	if err != nil || metadata == nil {
		return u
	}
	u.StartedAt = metadata.Timestamp
	u.Fields["delivery_count"] = int(metadata.NumDelivered)
	if metadata.Stream != "" || metadata.Consumer != "" || metadata.Sequence.Stream > 0 {
		u.Fields["nats"] = map[string]any{
			"stream":   metadata.Stream,
			"consumer": metadata.Consumer,
			"sequence": int64(metadata.Sequence.Stream),
		}
	}
	return u
}

// headerCarrier adapts the headers of one NATS message to the propagate.Carrier interface. NATS
// header keys compare exactly, so Get reads the key as it is written.
type headerCarrier nats.Header

// Get returns the first value of key, and an empty string when key is absent.
func (c headerCarrier) Get(key string) string {
	values := c[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// Set stores value under key.
func (c headerCarrier) Set(key, value string) { c[key] = []string{value} }

// Keys returns every key of the headers.
func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}
