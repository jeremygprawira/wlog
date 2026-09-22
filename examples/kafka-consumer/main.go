// Command kafka-consumer consumes one Kafka topic with wlog around every message. It is the
// kafka-consumer recipe's example: one event per message, a commit only after the handler
// returns nil, and the trace of the producer on the message headers.
package main

import (
	"context"
	"log"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	wlogkafkago "github.com/jeremygprawira/wlog/queue/kafkago"
)

// handle is the work of one message. The handler adds the field a searcher asks for.
func handle(ctx context.Context, msg kafka.Message) error {
	wlog.Set(ctx, "order_id", string(msg.Key))
	return nil
}

func main() {
	logger := wlog.New(wlog.WithService("kafka-consumer", "0.0.1", "local"))
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		GroupID: "orders",
		Topic:   "orders",
	})
	// Consume fetches one message, runs the handler inside one event, and commits the
	// message after the handler returns nil. A failed handler leaves the message
	// uncommitted, and the loop stops, because a reader moves its read position on every
	// fetch. Start a new reader to retry the failed message.
	for {
		if err := wlogkafkago.Consume(context.Background(), logger, reader, handle); err != nil {
			log.Printf("consume: %v", err)
			break
		}
	}
	// The process ends here, so the pending events are sent now.
	_ = logger.Flush(context.Background())
}
