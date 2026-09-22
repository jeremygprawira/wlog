// Package wlogcloudevents is wlog's CloudEvents adapter: one event of work per received event,
// and one call per sent event.
//
// Read top to bottom: Observability returns the service for
// client.WithObservabilityService. Recover wraps one CloudEvents function, so a panic becomes
// an error. EventDefaulter writes the trace of the context as extensions.
//
// This is the whole setup. Recover wraps a receiver function, because the SDK skips the
// observability callback of a function that panics. EventDefaulter writes the trace of the
// context, so a caller that sends an event outside the service joins the same trace.
//
//	client, _ := cloudevents.NewClient(protocol,
//		client.WithObservabilityService(wlogcloudevents.Observability(log)),
//		client.WithEventDefaulter(wlogcloudevents.EventDefaulter()),
//	)
//	client.StartReceiver(ctx, wlogcloudevents.Recover(handle))
package wlogcloudevents
