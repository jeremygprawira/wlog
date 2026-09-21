// Package wlogtemporal is wlog's Temporal adapter: one event per activity attempt, and no
// event from workflow code.
//
// A workflow must replay the same way on every run, so the interceptor leaves workflow code
// alone. An activity runs once per attempt, so every attempt gets its own event.
//
// Read top to bottom: Interceptor returns the worker interceptor, and the activity
// interceptor opens one job event around one activity attempt.
package wlogtemporal
