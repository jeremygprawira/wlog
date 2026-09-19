// Package wloghuma adds the huma operation id to the open request event.
//
// Read top to bottom: Middleware reads the operation huma matched and writes its id onto
// the event. It starts no event, so pair it with a router adapter from track A, such as
// wlogchi, which starts the event and reports the route:
//
//	r := chi.NewRouter()
//	r.Use(wlogchi.Middleware(log))
//	api := humachi.New(r, huma.DefaultConfig("orders", "1.0.0"))
//	api.UseMiddleware(wloghuma.Middleware())
package wloghuma

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/jeremygprawira/wlog"
)

// Middleware returns the huma middleware that records the operation id of the matched
// operation on the open event. Pass it to huma.API.UseMiddleware.
//
// The middleware runs after the handler, so the id lands before the router adapter emits
// the event. An operation with no id writes nothing.
func Middleware() func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		next(ctx)
		op := ctx.Operation()
		if op == nil || op.OperationID == "" {
			return
		}
		wlog.SetGroup(ctx.Context(), "http", "operation_id", op.OperationID)
	}
}
