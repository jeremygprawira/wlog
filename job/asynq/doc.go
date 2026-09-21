// Package wlogasynq is wlog's asynq adapter: one event per processed task, and one call per
// enqueue.
//
// Read top to bottom: Middleware and Handler wrap a task handler in one job event, unitOf
// maps a task and its context onto the unit of work, and Enqueue writes the trace headers of
// the context into the task and records one call.
package wlogasynq
