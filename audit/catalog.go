package audit

import (
	"context"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/catalog"
)

// Catalog returns a wlog.Enricher that reads the audit policy from a catalog registry.
// It finds the entry whose Audit.Action matches the event's audit action, fills
// target.type from the entry when it is empty, and, for an entry with ReasonRequired
// and an empty reason, marks the record with reason_missing.
//
// A missing reason never drops the record. Losing an audit fact is worse than keeping
// an incomplete one.
func Catalog(reg *catalog.Registry) wlog.Enricher {
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		records, ok := event[auditKey].([]any)
		if !ok {
			return
		}
		// Every record on the event gets the policy, not only the last one.
		for _, raw := range records {
			record, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			applyPolicy(record, policyFor(reg, actionOf(record)))
		}
	})
}

// actionOf returns one record's action.
func actionOf(record map[string]any) string {
	action, _ := record["action"].(string)
	return action
}

// applyPolicy fills target.type from the policy and marks a record that is missing a
// reason the policy requires. A nil policy leaves the record alone.
func applyPolicy(record map[string]any, auditPolicy *catalog.Audit) {
	if auditPolicy == nil {
		return
	}
	if auditPolicy.TargetType != "" {
		target, _ := record["target"].(map[string]any)
		if target == nil {
			target = map[string]any{}
			record["target"] = target
		}
		if target["type"] == nil || target["type"] == "" {
			target["type"] = auditPolicy.TargetType
		}
	}
	if auditPolicy.ReasonRequired {
		if reason, _ := record["reason"].(string); reason == "" {
			record["reason_missing"] = true
		}
	}
}

// policyFor returns the audit policy of the entry whose action matches, or nil.
func policyFor(reg *catalog.Registry, action string) *catalog.Audit {
	if reg == nil || action == "" {
		return nil
	}
	for _, code := range reg.Codes() {
		entry, ok := reg.Get(code)
		if !ok || entry.Audit == nil {
			continue
		}
		if entry.Audit.Action == action {
			return entry.Audit
		}
	}
	return nil
}
