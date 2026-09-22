// Package wlogamqp is wlog's RabbitMQ adapter: one event per delivery and one call per publish.
//
// Read top to bottom: Consume reads deliveries, acks one after the handler returns nil, and
// nacks a failed one. PublishWithContext records one call and writes the trace headers of the
// context.
// The setup line lives in docs/async-adapters.md, which `make snippets` compiles.
package wlogamqp
