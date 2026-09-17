package audit

import (
	"context"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/catalog"
	"github.com/jeremygprawira/wlog/redact"
)

// The rule names a record reports when it breaks its policy. A broken rule never drops the
// record: losing an audit fact is worse than keeping an incomplete one.
const (
	// ViolationReasonRequired means the policy needs a reason and the record has none.
	ViolationReasonRequired = "reason_required"
	// ViolationChangesRequired means the policy needs the changes and the record has none.
	ViolationChangesRequired = "changes_required"
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

// applyPolicy fills target.type from the policy, marks every rule the record breaks, and
// masks the values a redacted path names. A nil policy leaves the record alone.
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

	var violations []string
	reason, _ := record["reason"].(string)
	if auditPolicy.ReasonRequired && reason == "" {
		record["reason_missing"] = true
		violations = append(violations, ViolationReasonRequired)
	}
	changes, _ := record["changes"].([]any)
	if auditPolicy.RequiresChanges && len(changes) == 0 {
		violations = append(violations, ViolationChangesRequired)
	}
	if len(violations) > 0 {
		// A []any, not a []string: the event holds a JSON tree, and the redactor and every
		// drain walk only the tree shapes. A []string would escape both.
		list := make([]any, len(violations))
		for i, name := range violations {
			list[i] = name
		}
		record["violations"] = list
	}

	maskChanges(changes, auditPolicy.RedactPaths)
}

// maskChanges replaces the value of every change operation whose path the policy redacts,
// so a patch can describe a change to a secret without carrying the secret.
func maskChanges(changes []any, redactPaths []string) {
	if len(changes) == 0 || len(redactPaths) == 0 {
		return
	}
	mask := redact.Default().Replacement()
	for _, raw := range changes {
		op, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, hasValue := op["value"]; !hasValue {
			continue
		}
		path, _ := op["path"].(string)
		if pathMatchesPolicy(path, redactPaths) {
			op["value"] = mask
		}
	}
}

// pathMatchesPolicy reports whether a JSON Pointer names one of the policy's paths. An
// entry matches the operation's path exactly or as a prefix, so "user.creds" covers
// "user.creds.nik".
func pathMatchesPolicy(pointer string, redactPaths []string) bool {
	dotted := strings.ReplaceAll(strings.TrimPrefix(pointer, "/"), "/", ".")
	for _, entry := range redactPaths {
		if entry == "" {
			continue
		}
		if dotted == entry || strings.HasPrefix(dotted, entry+".") {
			return true
		}
	}
	return false
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
