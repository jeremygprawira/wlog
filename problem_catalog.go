// This file holds the catalog of problem codes: one entry per code, with the why, the
// fix, and the link to docs/problems.md. A test reads the catalog and proves that the
// document covers each code, so the two cannot drift apart.
package wlog

// Problem codes. A code is stable once it ships, because a caller matches on it and
// the documentation anchors a section to it.
const (
	codeNoEvent          = "WLOG_NO_EVENT"
	codeLateWrite        = "WLOG_LATE_WRITE"
	codeLoggerClosed     = "WLOG_LOGGER_CLOSED"
	codeHookPanic        = "WLOG_HOOK_PANIC"
	codeDrainSlow        = "WLOG_DRAIN_SLOW"
	codeDrainBackpressue = "WLOG_DRAIN_BACKPRESSURE"
	codeDrainFailed      = "WLOG_DRAIN_FAILED"
	codeDrainDropped     = "WLOG_DRAIN_DROPPED"
	codeDrainDisabled    = "WLOG_DRAIN_DISABLED"
	codeWriterDropped    = "WLOG_WRITER_DROPPED"
	codeEventTooLarge    = "WLOG_EVENT_TOO_LARGE"
	codeCapReached       = "WLOG_CAP_REACHED"
	codeValueUnencodable = "WLOG_VALUE_UNENCODABLE"
	codeInvalidConfig    = "WLOG_INVALID_CONFIG"
	codeAuditDisabled    = "WLOG_AUDIT_DISABLED"
	codeAuditWriteFailed = "WLOG_AUDIT_WRITE_FAILED"
	codeSilentNoDrain    = "WLOG_SILENT_NO_DRAIN"
	codeEventDropped     = "WLOG_EVENT_DROPPED"
)

// problemsURL is the page every code links to. The anchor is the code in lower case,
// because GitHub lowercases a heading anchor.
const problemsURL = "https://github.com/jeremygprawira/wlog/blob/main/docs/problems.md#"

// problemCatalog lists every code wlog reports, in the order docs/problems.md holds
// them. Why says when the code fires, and Fix says what the caller does about it.
var problemCatalog = withLinks([]Problem{
	{
		Code: codeNoEvent,
		Why:  "A write needs an event, and the context holds none.",
		Fix:  "Start an event with Start or Detach, and write on the context it returns.",
	},
	{
		Code: codeLateWrite,
		Why:  "A write lands on an event that already emitted, and no parent event is open.",
		Fix:  "Write before the end func runs. For work that outlives the request, use Detach.",
	},
	{
		Code: codeLoggerClosed,
		Why:  "An event emits after Close, so no drain receives it.",
		Fix:  "Emit every event before Close, and Close after the last request ends.",
	},
	{
		Code: codeHookPanic,
		Why:  "A user hook panicked, and the hook name is in Source.",
		Fix:  "Fix the hook. Core recovers, so the panic affects the hook alone.",
	},
	{
		Code: codeDrainSlow,
		Why:  "A synchronous drain held the emitting goroutine for more than five milliseconds.",
		Fix:  "Send on a buffered pipeline. See pipeline.Wrap.",
	},
	{
		Code: codeDrainBackpressue,
		Why:  "A backend accepted events and warned that it is near its capacity.",
		Fix:  "Slow the send rate, or ask the backend for more capacity.",
	},
	{
		Code: codeDrainFailed,
		Why:  "A drain gave up on a batch, after its own retries.",
		Fix:  "Read Err for the backend answer, then check the endpoint and the credentials.",
	},
	{
		Code: codeDrainDropped,
		Why:  "A drain buffer overflowed and dropped events.",
		Fix:  "Raise the buffer, or make the backend keep up.",
	},
	{
		Code: codeDrainDisabled,
		Why:  "setup.FromEnv skipped a drain because a required variable is missing.",
		Fix:  "Set the variables from the drain's documents, or remove the drain from the setup.",
	},
	{
		Code: codeWriterDropped,
		Why:  "The async writer queue is full, so it dropped its oldest line.",
		Fix:  "Log less on the console, or raise the queue with WriterBuffer.",
	},
	{
		Code: codeEventTooLarge,
		Why:  "The event was over the size cap, so finalize removed fields to fit.",
		Fix:  "Store large values outside the event, and keep a key for the reference.",
	},
	{
		Code: codeCapReached,
		Why:  "A key, group, array, log, error, call, or audit cap dropped a value.",
		Fix:  "Send fewer values, or split the work into more than one event.",
	},
	{
		Code: codeValueUnencodable,
		Why:  "A value became an [unencodable] marker, because no encoder accepted it.",
		Fix:  "Store a value that JSON holds, such as a string or a number.",
	},
	{
		Code: codeInvalidConfig,
		Why:  "An env var or an option held a value wlog cannot use.",
		Fix:  "Read the message for the variable and the reason, then set a valid value.",
	},
	{
		Code: codeAuditDisabled,
		Why:  "A disabled Logger dropped an audit record.",
		Fix:  "Keep logging on for the process that writes audit records.",
	},
	{
		Code: codeAuditWriteFailed,
		Why:  "The audit journal failed to write or to sync.",
		Fix:  "Check the journal path, its permissions, and the free space on the disk.",
	},
	{
		Code: codeSilentNoDrain,
		Why:  "WithSilent is set, and the Logger has no drain, so events go nowhere.",
		Fix:  "Add a drain, or drop WithSilent.",
	},
	{
		Code: codeEventDropped,
		Why:  "Debug mode only. An event was dropped, and Why names the reason.",
		Fix:  "Nothing, unless the drop is unexpected. Then read Why and adjust the filter.",
	},
})

// problemByCode finds one catalog entry. It reports false for an unknown code, so a
// caller may report a code that this version does not know.
func problemByCode(code string) (Problem, bool) {
	for _, p := range problemCatalog {
		if p.Code == code {
			return p, true
		}
	}
	return Problem{}, false
}

// withLinks returns the catalog with the link of every entry filled from its code, so
// the table holds no repeated URL text and no code writes it later.
func withLinks(catalog []Problem) []Problem {
	for i := range catalog {
		catalog[i].Link = problemsURL + anchor(catalog[i].Code)
	}
	return catalog
}

// anchor turns a code into the anchor that GitHub builds for its heading, which is
// the lower-case code.
func anchor(code string) string {
	out := make([]rune, 0, len(code))
	for _, r := range code {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		out = append(out, r)
	}
	return string(out)
}
