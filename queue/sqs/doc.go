// Package wlogsqs is wlog's AWS SQS and SNS adapter: one event per received message and one
// call per publish.
//
// Read top to bottom: Receive reads messages in a loop, gives each one an event, and deletes a
// message after the handler returns nil. SendMessage and Publish record one call and write the
// trace attributes of the context.
// The setup line lives in docs/async-adapters.md, which `make snippets` compiles.
package wlogsqs
