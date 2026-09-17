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

	// Version is this record's schema version. Zero means 1.
	Version int `json:"version,omitempty"`
	// IdempotencyKey dedupes a retried write.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// Context carries free-form facts, such as a request id or a ticket.
	Context map[string]any `json:"context,omitempty"`
	// ErrorCode is the code of the error behind an "error" outcome, set by Wrap.
	ErrorCode string `json:"error_code,omitempty"`
}

// versionOf fills a zero Version with 1, the current schema version.
func versionOf(r Record) int {
	if r.Version == 0 {
		return 1
	}
	return r.Version
}

// Do records r. Inside an active wlog.Start, it adds r to the "audit" array on that
// event, so the audit fact rides along with the rest of the request's data and a second
// Do adds a record instead of replacing the first. The array holds up to 20 records, the
// cap SPEC.md sets. Outside an active event, or after its end ran, Do opens and
// immediately closes its own event via wlog.Start(ctx, "audit."+r.Action), so an audit
// call never depends on the caller already being inside a request, and never becomes a
// late write into a sealed event.
//
// The "audit" array always has room on the event, even when the event already holds the
// full set of top-level keys. Losing an audit record to a key cap would be a hole in a
// chain that a reader must be able to verify (gate G5).
func Do(ctx context.Context, r Record) {
	r.Version = versionOf(r)
	if wlog.HasEvent(ctx) {
		wlog.Append(ctx, auditKey, r)
		return
	}
	ctx, end := wlog.Start(ctx, "audit."+r.Action)
	defer end()
	wlog.Append(ctx, auditKey, r)
}

// auditKey is the reserved field that carries the audit records.
const auditKey = "audit"
