// Package audit records "who did what, to what, with what outcome" using the normal
// wlog event pipeline, so audit data gets the same redaction and drains as everything
// else instead of a separate logging path. Do sets the reserved "audit" field, which
// core already force-keeps past any sampler (SPEC.md gate G5).
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Actor identifies who performed the action. Type names one of the four types in
// actor.go. An agent actor also fills Model, Tools, and PromptID, so a review can tell
// which model, which tools, and which prompt were behind the action.
type Actor struct {
	Type  string `json:"type,omitempty"`
	ID    string `json:"id,omitempty"`
	Email string `json:"email,omitempty"`

	// Model is the model an agent ran, such as "claude-sonnet-4-5".
	Model string `json:"model,omitempty"`
	// Tools lists the tools an agent could call for this action.
	Tools []string `json:"tools,omitempty"`
	// PromptID names the prompt version an agent ran.
	PromptID string `json:"prompt_id,omitempty"`
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
	// IdempotencyKey dedupes a retried write. Do fills it when the caller leaves it
	// empty, with a key that is stable for one request id.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// CorrelationID ties every record of one request together across services. Do fills
	// it from trace.request_id when the caller leaves it empty.
	CorrelationID string `json:"correlation_id,omitempty"`
	// CausationID names the event that caused this action. Do fills it from
	// trace.parent_event_id when the caller leaves it empty.
	CausationID string `json:"causation_id,omitempty"`
	// Changes holds the RFC 6902 operations that describe what the action changed, as
	// audit.Patch builds them.
	Changes []Operation `json:"changes,omitempty"`
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
	r = withDefaults(ctx, r)
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

// withDefaults fills the fields a caller left empty. The correlation and causation ids
// come from the event's trace group, so a record joins the request it belongs to without
// the caller copying anything by hand.
func withDefaults(ctx context.Context, r Record) Record {
	requestID := traceField(ctx, "request_id")
	if r.CorrelationID == "" {
		r.CorrelationID = requestID
	}
	if r.CausationID == "" {
		r.CausationID = traceField(ctx, "parent_event_id")
	}
	if r.IdempotencyKey == "" {
		r.IdempotencyKey = idempotencyKey(r, requestID)
	}
	return r
}

// traceField reads one field of the current event's trace group, or "" when the event
// holds none.
func traceField(ctx context.Context, name string) string {
	group, ok := wlog.Field(ctx, "trace")
	if !ok {
		return ""
	}
	trace, ok := group.(map[string]any)
	if !ok {
		return ""
	}
	value, _ := trace[name].(string)
	return value
}

// idempotencyKey returns a key that is stable for one request: the first 32 hex characters
// of a SHA-256 over the action, the actor, the target, and the request id. A retried
// request with the same id therefore records the same key, and a different request does
// not.
func idempotencyKey(r Record, requestID string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		r.Action, r.Actor.ID, r.Target.Type, r.Target.ID, requestID,
	}, "\x00")))
	return hex.EncodeToString(sum[:])[:32]
}
