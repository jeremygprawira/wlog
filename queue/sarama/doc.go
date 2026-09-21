// Package wlogsarama is wlog's sarama adapter: one event per consumed message and one call
// per produced message.
//
// Read top to bottom: Handler implements sarama.ConsumerGroupHandler and owns the claim
// loop, so it marks a message after the handler succeeds. Message is the helper for a loop
// that a caller owns. SyncProducer and AsyncProducer wrap a producer, add the trace headers
// of the context, and record one call per send.
package wlogsarama
