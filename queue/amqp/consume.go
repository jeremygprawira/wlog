// This file holds the consumer side: the loop over a delivery channel, the ack rule, and the
// mapping from one delivery onto one unit of work.
package wlogamqp

import (
	"context"
	"errors"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

// Handler handles one delivery inside its event.
type Handler func(ctx context.Context, delivery amqp.Delivery) error

// Option configures one Consume call.
type Option func(*config)

// config holds the resolved options of one Consume call.
type config struct {
	requeueOn []error
}

// RequeueOn names the errors that make Consume requeue a failed delivery. A failed delivery whose
// error matches none of them is nacked without requeue, so a dead-letter exchange can take it.
// With no RequeueOn, every failed delivery requeues.
func RequeueOn(errs ...error) Option {
	return func(c *config) { c.requeueOn = append(c.requeueOn, errs...) }
}

// Consume reads deliveries until the channel closes or the context ends. Each delivery gets one
// event, and Consume acks it after the handler returns nil. A failed delivery is nacked, with a
// requeue unless RequeueOn named the error. The channel must come from a consumer with
// autoAck false, or the broker acks every delivery before the handler runs. A nil Logger means
// wlog.Default.
func Consume(ctx context.Context, log *wlog.Logger, deliveries <-chan amqp.Delivery, queue string, fn Handler, opts ...Option) error {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}
			err := work.Run(ctx, log, unitOf(delivery, queue), func(ctx context.Context) error {
				return fn(ctx, delivery)
			})
			settle(delivery, err, cfg)
		}
	}
}

// settle acks or nacks one delivery, by the outcome of its handler.
func settle(delivery amqp.Delivery, err error, cfg config) {
	if delivery.Acknowledger == nil {
		return
	}
	if err == nil {
		_ = delivery.Ack(false)
		return
	}
	_ = delivery.Nack(false, requeue(err, cfg))
}

// requeue reports whether a failed delivery goes back to the queue.
func requeue(err error, cfg config) bool {
	if len(cfg.requeueOn) == 0 {
		return true
	}
	for _, target := range cfg.requeueOn {
		if target != nil && errors.Is(err, target) {
			return true
		}
	}
	return false
}

// unitOf maps one delivery onto a unit of work. The timestamp becomes the start time, so the
// event carries the time the message waited as lag_ms. The exchange and the routing key go under
// messaging.rabbitmq, because the work table has no field for them.
func unitOf(delivery amqp.Delivery, queue string) work.Unit {
	fields := map[string]any{
		"system":    "rabbitmq",
		"operation": "process",
	}
	if queue != "" {
		fields["destination"] = queue
	}
	if delivery.MessageId != "" {
		fields["message_id"] = delivery.MessageId
	}
	if delivery.Redelivered {
		fields["redelivered"] = true
	}
	rabbitmq := map[string]any{}
	if delivery.Exchange != "" {
		rabbitmq["exchange"] = delivery.Exchange
	}
	if delivery.RoutingKey != "" {
		rabbitmq["routing_key"] = delivery.RoutingKey
	}
	if len(rabbitmq) > 0 {
		fields["rabbitmq"] = rabbitmq
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   tableCarrier(delivery.Headers),
		StartedAt: delivery.Timestamp,
	}
}

// tableCarrier adapts the headers of one delivery to the propagate.Carrier interface. A value
// that a publisher wrote as a string reads as a string, and a value that arrived as bytes from
// another client reads as text too.
type tableCarrier amqp.Table

// Get returns the text of one header, and an empty string when the header is absent.
func (c tableCarrier) Get(key string) string {
	switch value := c[key].(type) {
	case string:
		return value
	case []byte:
		return string(value)
	}
	return ""
}

// Set stores value under key as a string, which is the type a reading client expects.
func (c tableCarrier) Set(key, value string) { c[key] = value }

// Keys returns every key of the headers.
func (c tableCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}
