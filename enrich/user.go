package enrich

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// User adds user.id from fn(ctx), when it returns a non-empty string. A panic in fn is
// isolated by core's own enricher dispatch, so no extra handling is needed here.
//
// User(Overwrite(true)) replaces a user.id the handler already set. The default leaves the
// handler's own id alone, because the handler knows more about the request than a lookup does.
func User(fn func(ctx context.Context) string, opts ...Option) wlog.Enricher {
	cfg := newConfig(opts)
	return wlog.EnricherFunc(func(ctx context.Context, event map[string]any) {
		uid := fn(ctx)
		if uid == "" {
			return
		}
		mergeGroup(event, "user", map[string]any{"id": uid}, cfg.overwrite)
	})
}
