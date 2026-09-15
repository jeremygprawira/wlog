// Package audit records "who did what, to what, with what outcome" using the normal
// wlog event pipeline, so audit data gets the same redaction and drains as everything
// else instead of a separate logging path. Do sets the reserved "audit" field, which
// core already force-keeps past any sampler (SPEC.md gate G5).
package audit

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// Actor identifies who performed the action.
type Actor struct {
	Type  string `json:"type,omitempty"`
	ID    string `json:"id,omitempty"`
	Email string `json:"email,omitempty"`
}

// Target identifies what the action was performed on.
type Target struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

// Record is one audit fact: who, what action, on what, and the result.
type Record struct {
	Actor   Actor  `json:"actor"`
	Action  string `json:"action"`
	Target  Target `json:"target"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// Do records r. Inside an active wlog.Start, it sets the "audit" field on that event so
// the audit fact rides along with the rest of the request's data. Outside one, it opens
// and immediately closes its own event via wlog.Start(ctx, "audit."+r.Action), so an
// audit call never depends on the caller already being inside a request.
func Do(ctx context.Context, r Record) {
	if wlog.HasEvent(ctx) {
		wlog.Set(ctx, "audit", r)
		return
	}
	ctx, end := wlog.Start(ctx, "audit."+r.Action)
	defer end()
	wlog.Set(ctx, "audit", r)
}
