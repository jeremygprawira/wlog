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
	Audit   *Audit // nil when the code needs no audit record
}

// Error makes an Entry usable as an errors.Is target. errors.Is(err, entry) is true
// when err carries that entry's code.
func (e Entry) Error() string { return e.Message }

// Audit is the audit policy for one code. The audit module reads it.
type Audit struct {
	Action     string // such as "invoice.refund"
	TargetType string // such as "invoice"
	Severity   string // "low", "medium", "high", or "critical"
	// ReasonRequired true means an audit record with an empty Reason is marked
	// reason_missing. The record is never dropped: an incomplete fact is worth more
	// than a lost one.
	ReasonRequired bool
}
