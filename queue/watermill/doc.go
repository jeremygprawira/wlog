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
package wlogwatermill
