// Package wlogriver is wlog's River adapter: one event per worked job, and one call per
// insert.
//
// River is pre-1.0 and carries the MPL-2.0 license, so a user of this package takes that
// license with the dependency.
//
// Read top to bottom: New returns the worker middleware that opens one job event, and
// InsertMiddleware returns the insert middleware that records one call and writes the trace
// of the context into the job metadata.
package wlogriver
