package audit

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// forwardedKeys are the only keys an audit-only drain forwards, besides the audit records
// themselves. They identify the event: when it happened, which event it was, which service
// produced it, and which trace it belongs to. event_id arrives with the phase 11 event
// shape, and is forwarded once the event carries one.
var forwardedKeys = []string{"timestamp", "event_id", "service", "trace"}

// OnlyDrain returns a wlog.Drain that forwards only the audit fact of each event it
// receives, and drops every event that holds no audit record.
//
// Use it in front of a compliance backend that must not receive the rest of the request's
// data: the forwarded event holds timestamp, event_id, service, trace, and audit, and
// nothing else. The audit records themselves are the same maps the redactor already
// masked, so a denied value cannot reach the backend through this path.
func OnlyDrain(next wlog.Drain) wlog.Drain {
	return wlog.DrainFunc(func(ctx context.Context, event map[string]any) {
		raw, ok := event[auditKey].([]any)
		if !ok || len(raw) == 0 {
			return
		}
		// The slice is copied, so replacing the source event's array later cannot change
		// what this drain already sent. The record maps inside are read-only.
		forwarded := map[string]any{auditKey: append([]any(nil), raw...)}
		for _, key := range forwardedKeys {
			if value, ok := event[key]; ok {
				forwarded[key] = value
			}
		}
		next.Send(ctx, forwarded)
	})
}
