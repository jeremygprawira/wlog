// This file holds the drain side: it ships every finished event to a Kafka topic, and it
// builds itself from the environment for setup.
package wlogkafka

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/setup"
)

// Drain returns a wlog.Drain that writes each event as one Kafka message to the topic of w.
// The events batch and retry through pipeline.Wrap, and Close sends what is pending.
func Drain(w *kafka.Writer) wlog.Drain {
	return pipeline.Wrap(sender{w: w})
}

// sender writes one batch of events as Kafka messages.
type sender struct {
	w *kafka.Writer
}

// SendBatch writes one message per event.
func (s sender) SendBatch(ctx context.Context, events []map[string]any) error {
	msgs := make([]kafka.Message, 0, len(events))
	for _, event := range events {
		body, err := json.Marshal(event)
		if err != nil {
			return err
		}
		msgs = append(msgs, kafka.Message{Value: body})
	}
	return s.w.WriteMessages(ctx, msgs...)
}

// Factory returns the setup factory of the Kafka drain, so a deployment picks it with
// WLOG_DRAINS=kafka. It reads WLOG_KAFKA_BROKERS, a comma separated list of addresses, and
// WLOG_KAFKA_TOPIC. SASL, TLS, and credentials need code.
func Factory() setup.Factory {
	return setup.Factory{
		Name: "kafka",
		Vars: []setup.Var{
			{Name: "WLOG_KAFKA_BROKERS", Required: true},
			{Name: "WLOG_KAFKA_TOPIC", Required: true},
		},
		New: func(env setup.Env) (wlog.Drain, error) {
			brokers, _ := env.Lookup("WLOG_KAFKA_BROKERS")
			topic, _ := env.Lookup("WLOG_KAFKA_TOPIC")
			addrs := splitList(brokers)
			if len(addrs) == 0 || topic == "" {
				return nil, errors.New("wlogkafka: WLOG_KAFKA_BROKERS and WLOG_KAFKA_TOPIC are both required")
			}
			w := &kafka.Writer{Addr: kafka.TCP(addrs...), Topic: topic}
			return Drain(w), nil
		},
	}
}

// splitList splits a comma separated list of addresses and drops empty entries.
func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
