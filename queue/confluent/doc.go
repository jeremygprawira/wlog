// Package wlogconfluent is wlog's confluent-kafka-go adapter: one event per consumed message
// and one call per produced message.
//
// The adapter imports confluent-kafka-go, which needs cgo. Every file except this one
// carries a cgo build tag, so a build with CGO_ENABLED=0 compiles this empty package and no
// cgo code.
//
// Read top to bottom: Consume reads messages in a loop, commits a message after the handler
// returns nil, and leaves a failed message uncommitted. Produce sends one message and ends
// its call on the delivery report.
// The setup line lives in docs/async-adapters.md, which `make snippets` compiles.
package wlogconfluent
