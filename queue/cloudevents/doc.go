// Package wlogcloudevents is wlog's CloudEvents adapter: one event of work per received event,
// and one call per sent event.
//
// Read top to bottom: Observability returns the service for client.WithObservabilityService,
// Recover wraps one CloudEvents function so a panic becomes an error, and EventDefaulter writes
// the trace headers of the context as extensions.
//
// This is the whole setup:
//
//	client, _ := cloudevents.NewClient(protocol, client.WithObservabilityService(wlogcloudevents.Observability(log)))
package wlogcloudevents
