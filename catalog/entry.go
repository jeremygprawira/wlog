// Package catalog holds one registry per domain for the static facts about an error
// code: its status, its message template, its repair guidance, and the audit policy
// that applies to it. A registry decorates whatever wlog.ErrorExtractor is already in
// use, so a herr user, a standard-errors user, and a custom-extractor user get the
// same result. The package imports nothing outside the standard library.
package catalog

// Entry is one code's static facts. A registry copies it, so a caller cannot change an
// entry after New returns.
type Entry struct {
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
	Action         string // such as "invoice.refund"
	TargetType     string // such as "invoice"
	Severity       string // "low", "medium", "high", or "critical"
	ReasonRequired bool   // true means audit.Do rejects an empty Reason
}
