// This file holds the batch helpers: one parent event for a group of messages, with the
// size of the batch, the count of the failures, and one linked event per message.
package work

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// BatchEvent opens the parent event of one batch: a unit of work of the kind in u with
// batch_size set to n.
//
// A handler calls it once, then Start once per message with the returned context. Every
// message event of that context links to the parent with trace.parent_event_id, because a
// unit of work that starts inside another one becomes its child.
func BatchEvent(ctx context.Context, log *wlog.Logger, u Unit, n int) (context.Context, *Handle) {
	fields := make(map[string]any, len(u.Fields)+1)
	for key, value := range u.Fields {
		fields[key] = value
	}
	fields["batch_size"] = n
	u.Fields = fields
	return Start(ctx, log, u)
}

// Failed counts one failed item of the batch in batch_failures, so the parent event says
// how much of the batch worked.
func (h *Handle) Failed() {
	h.failures++
	h.Set("batch_failures", h.failures)
}
