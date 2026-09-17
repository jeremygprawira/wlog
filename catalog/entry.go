// Package catalog holds one registry per domain for the static facts about an error
// code: its status, its message template, its repair guidance, and the audit policy
// that applies to it. A registry decorates whatever wlog.ErrorExtractor is already in
// use, so a herr user, a standard-errors user, and a custom-extractor user get the
// same result. The package imports nothing outside the standard library.
package catalog

// Entry is one code's static facts. A registry copies it, so a caller cannot change an
// entry after New returns.
type Entry struct {
	// domain is the registry prefix the entry belongs to, and full is the prefixed code.
	// New stamps both, so errors.Is can tell two entries with the same short code in
	// different domains apart, and so a lookup by the full code is exact.
	domain  string
	full    string
	Code    string // short code inside the domain, such as "not_found"
	Kind    string // maps to wlog.ErrorInfo.Kind
	Status  int    // HTTP status, 0 means unset
	Message string // template, with {name} placeholders filled from params
	Why     string
	Fix     string
	Link    string
	// Data holds defaults that are safe to send back to a client, such as a rejected
	// field name. It merges under the values a per-request extractor filled.
	Data map[string]any
	// Internal holds log-only defaults, such as a row id. It merges the same way.
	Internal map[string]any
	Audit    *Audit // nil when the code needs no audit record
}

// clone returns a copy of the entry that shares nothing with the original: the Audit and
// both default maps are copied, at every depth, so a caller can never change the registry
// by changing what it received.
func (e Entry) clone() Entry {
	out := e
	if e.Audit != nil {
		audit := *e.Audit
		audit.RedactPaths = append([]string(nil), e.Audit.RedactPaths...)
		out.Audit = &audit
	}
	out.Data = copyTree(e.Data)
	out.Internal = copyTree(e.Internal)
	return out
}

// copyTree copies a JSON tree of maps and slices, so the copy shares no map or slice with
// the original.
func copyTree(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = copyValue(item)
	}
	return out
}

// copyValue copies one value of a JSON tree.
func copyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return copyTree(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = copyValue(item)
		}
		return out
	default:
		return value
	}
}

// Error makes an Entry usable as an errors.Is target. errors.Is(err, entry) is true
// when err carries that entry's code.
func (e Entry) Error() string { return e.Message }

// Audit is the audit policy for one code. The audit module reads it.
type Audit struct {
	Action     string // such as "invoice.refund"
	TargetType string // such as "invoice"
	Severity   string // "low", "medium", "high", or "critical"
	// Description says what the action does, in one sentence, for a reader or a catalogue
	// page. It is never part of an event.
	Description string
	// ReasonRequired true means a record with an empty Reason breaks a rule.
	ReasonRequired bool
	// RequiresChanges true means a record must carry the changes it made.
	RequiresChanges bool
	// RedactPaths names the paths inside a record's changes whose values must be masked,
	// because the change itself is worth recording and the value behind it is not.
	RedactPaths []string
	// A record that breaks a rule is never dropped: it is kept and marked with the rule
	// names it broke, because an incomplete fact is worth more than a lost one.
}
