// Package wlognats is wlog's NATS adapter: one event per delivered message, one call per
// publish, and a drain that ships events to a subject.
//
// Read top to bottom: Handler wraps a core NATS subscription handler, JetStreamHandler wraps a
// JetStream consumer handler and owns the ack, Publish and PublishMsg record one call, and
// Drain ships finished events.
package wlognats
