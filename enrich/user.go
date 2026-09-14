package enrich

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// User adds user.id from fn(ctx), when it returns a non-empty string. A panic in fn
// is isolated by core's own enricher dispatch — no extra handling needed here.
func User(fn func(ctx context.Context) string) wlog.Enricher {
	return wlog.EnricherFunc(func(ctx context.Context, event map[string]any) {
		uid := fn(ctx)
		if uid == "" {
			return
		}
		mergeGroup(event, "user", map[string]any{"id": uid}, false)
	})
}
