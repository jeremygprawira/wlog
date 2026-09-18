# Problem codes

wlog reports its own failures as a `wlog.Problem`. A problem carries a stable code, the
source that noticed the fault, one message, and the why and fix text from this page.

<!-- snippet:sketch -->
```go
log := wlog.New(wlog.OnProblem(func(p wlog.Problem) {
	fmt.Fprintln(os.Stderr, p.Code, p.Source, p.Message)
}))
```

Without `OnProblem`, the default handler writes one JSON line to standard error. It
writes at most one line per code per minute, and it folds the rest into that line's
`count`.

`wlog.Problems()` returns this catalog. A test reads that catalog. A code with no
section on this page fails the test.

## WLOG_NO_EVENT

**Why.** A write needs an event, and the context holds none. The key name is in the message.

**Fix.** Start an event with `Start` or `Detach`, and write on the context those return. A
background write on a plain context is the usual cause.

<!-- snippet:sketch -->
```go
ctx, end := wlog.Start(ctx, "checkout")
defer end()
wlog.Set(ctx, "order_id", id)
```

## WLOG_LATE_WRITE

**Why.** A write lands on an event that already emitted. No parent event is open, so the
value has nowhere to go.

**Fix.** Write before the end func runs. For work that outlives the request, start the
child with `Detach` and end it after the work is done.

## WLOG_LOGGER_CLOSED

**Why.** An event emits after `Close`, so no drain receives it.

**Fix.** Emit every event before `Close`, and call `Close` after the last request ends.

## WLOG_HOOK_PANIC

**Why.** A user hook panicked. `Source` names the hook, so an Enricher, a Keeper, a
Plugin, or an error extractor.

**Fix.** Fix the hook. Core recovers the panic, so the failure stays inside that hook.

## WLOG_DRAIN_SLOW

**Why.** A synchronous drain held the emitting goroutine for over five milliseconds.

**Fix.** Send through a buffered pipeline. `pipeline.Wrap(d, pipeline.BatchSize(100))`
moves the send off the request goroutine.

## WLOG_DRAIN_BACKPRESSURE

**Why.** A backend accepted events and warned that it is near its capacity. `Err` holds
the backend's answer.

**Fix.** Slow the send rate, or ask the backend for more capacity. A dropped batch costs
more than a slower send.

## WLOG_DRAIN_FAILED

**Why.** A drain gave up on a batch after its own retries, or a drain panicked.

**Fix.** Read `Err` for the backend answer. Then check the endpoint, the credentials, and
the network path.

## WLOG_DRAIN_DROPPED

**Why.** A drain buffer overflowed, so events were dropped.

**Fix.** Raise the buffer, or make the backend keep up. `Stats` counts the drops.

## WLOG_DRAIN_DISABLED

**Why.** `setup.FromEnv` skipped a drain because a required variable is missing.

**Fix.** Set the variables from the drain's documents, or leave that drain out of the
setup. The message names the variable.

## WLOG_WRITER_DROPPED

**Why.** The async writer queue is full, so the oldest line was dropped. A slow terminal
or a full pipe causes this.

**Fix.** Write fewer lines to the console, or raise the queue with `WriterBuffer`.

## WLOG_EVENT_TOO_LARGE

**Why.** The event was larger than the size cap, so finalize removed fields to fit.

**Fix.** Store large values outside the event, and keep a reference key. `wlog.truncated`
names what was removed.

## WLOG_CAP_REACHED

**Why.** A key, group, array, log, error, call, or audit cap dropped a value.

**Fix.** Send fewer values, or split the work into more than one event. The counters on
the event name each cap that fired.

## WLOG_VALUE_UNENCODABLE

**Why.** A value became an `[unencodable]` marker, because no encoder accepted it. A
channel, a function, or a cyclic value causes this.

**Fix.** Store a value that JSON holds, such as a string, a number, or a map of those.

## WLOG_INVALID_CONFIG

**Why.** An env var or an option held a value wlog cannot use. The message names the
variable and the reason.

**Fix.** Set a valid value. wlog keeps the default and carries on, so a bad variable never
stops the process.

## WLOG_AUDIT_DISABLED

**Why.** A disabled Logger dropped an audit record, so the record of who did what is
missing.

**Fix.** Keep logging on for a process that writes audit records. Audit records are the
one thing a level filter never removes.

## WLOG_AUDIT_WRITE_FAILED

**Why.** The audit journal failed to write or to sync.

**Fix.** Check the journal path, its permissions, and the free space on the disk. An audit
record that cannot be written is reported rather than lost silently.

## WLOG_SILENT_NO_DRAIN

**Why.** `WithSilent` is set, and the Logger has no drain, so every event goes nowhere.
This is usually a setup mistake.

**Fix.** Add a drain, or drop `WithSilent`.

## WLOG_EVENT_DROPPED

**Why.** Debug mode only. An event was dropped, and `Why` names the reason: `sampled`,
`level`, `disabled`, `closed`, or `too_large`. Turn debug on with `WithDebug(true)` or
`WLOG_DEBUG=1`.

**Fix.** Nothing, unless the drop is unexpected. Then read `Why` and adjust the filter or
the sample rate.
