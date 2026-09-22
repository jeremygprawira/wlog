// Package wlogwatermill is wlog's Watermill adapter: one event per handled message and one
// call per publish.
//
// Read top to bottom: Middleware wraps a router handler, and PublisherDecorator wraps a
// publisher. Add Middleware before the other router middleware, so it is outermost and sees
// what they decided.
//
// This is the whole setup:
//
//	router.AddMiddleware(wlogwatermill.Middleware(log))
//	router.AddPublisherDecorators(wlogwatermill.PublisherDecorator())
//
// A handler that produces a message sets the event context on it, so the next service joins
// the trace of the unit:
//
//	out := message.NewMessage(uuid.NewString(), nil)
//	out.SetContext(msg.Context())
//
// The router publishes a produced message after the handler event ends. A publish in that
// window writes the trace of the unit and records no call, because the event is over.
package wlogwatermill
