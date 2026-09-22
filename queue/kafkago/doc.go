// Package wlogkafka is wlog's kafka-go adapter: one event per consumed message, one call
// per produced write, and a drain that ships events to a topic.
//
// Read top to bottom: Consume fetches one message, runs a handler inside one event, and
// commits the message after the handler succeeds. Writer wraps a *kafka.Writer, records
// one call per write, and adds the trace headers of the unit of work. Drain ships finished
// events to a topic, and Factory builds that drain from the environment.
//
// This is the whole setup for a consumer:
//
//	r := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, GroupID: "workers", Topic: "orders"})
//	for {
//		err := wlogkafka.Consume(ctx, log, r, handle)
//		if err != nil {
//			// A failed message is not committed, so the caller decides to retry it
//			// or to stop.
//			return err
//		}
//	}
package wlogkafkago
