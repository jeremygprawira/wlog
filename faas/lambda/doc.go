// Package wloglambda is wlog's AWS Lambda adapter: one function event per invocation, one
// message event per batch record, and a flush before the runtime freezes the process.
//
// A Lambda container stays warm, so the drains stay open and every invocation flushes. The
// event source mapping of a batch function must set ReportBatchItemFailures, or AWS ignores
// the returned failure list and retries the whole batch.
//
// Read top to bottom: Wrap gives every invocation one event, ProcessSQS, ProcessKinesis,
// and ProcessDynamoDB give every record of a batch its own event, and SIGTERMFlush flushes
// the drains on the spindown signal.
// The setup line lives in docs/async-adapters.md, which `make snippets` compiles.
package wloglambda
