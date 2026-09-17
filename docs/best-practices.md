# Best practices

Five rules for a wide event that earns its keep. Each rule points at the test that
proves the code does what this page says.

## What to put on an event, and what to leave off

Put on the event what answers a question later: the operation, its outcome, the ids
that name the work, the timings, and the failure detail. Leave off a secret, and leave
off a copy of a whole request body you will never read.

The redactor is the backstop, not the plan. A denied key is masked before any sink sees
it, proven by `FuzzRedact_NeverLeaks` and by one leak test per drain.

```go
wlog.Set(ctx, "order_id", order.ID)
wlog.Error(ctx, err)
```

## Name a field one way

Use `snake_case`, and put a group in front of its fields, so `http.status` and
`payment.method` never collide. Declare a `wlog.Key[T]` for a field you set often: a
wrong value type then fails to compile.

```go
var OrderID = wlog.NewKey[string]("order_id")
OrderID.Set(ctx, order.ID)
```

`TestCore_Key_SetStoresUnderItsName` proves the type check, and
`TestCore_StrictKeys_FlagsUnregisteredKey_InDev` proves that a typo is flagged in a dev
environment.

## Open a Detach for work that outlives the response

A `Detach` starts its own event for work that continues after the response is sent: an
email, a webhook, a report. It copies the trace link, so the child still points at the
parent. Do not keep writing to the request's event after `end` ran; that write is late
and only counted.

`TestDetachExample_ChildOutlivesParent` proves the link.

## Pick a sampling rate from the question you ask

Head sampling drops events before they are built. Tail sampling keeps the ones you would
have wanted anyway. Start with `sample.KeepErrorsAndSlow`: keep every error and every
slow request, then a small share of the rest. Errors are always force-kept, so a low
rate can never hide a failure.

```go
wlog.WithSampler(sample.MustNew(
    sample.Rate(wlog.LevelInfo, 10),
    sample.KeepDuration(time.Second),
))
```

`TestCore_StageOrder_AuditBypassesSampling` proves an audit event survives a 0% sampler.

## Record an audit fact, not a log line

An audit fact says who did what, to what, and with what outcome. A log line says what the
code did. If an auditor would ask for it, it is an audit record: login, role change,
refund, export, deletion.

```go
audit.Do(ctx, audit.Record{
    Actor:   audit.Actor{Type: "user", ID: user.ID},
    Action:  "invoice.refund",
    Target:  audit.Target{Type: "invoice", ID: invoice.ID},
    Outcome: "success",
    Reason:  "customer request",
})
```

An audit record always passes sampling, is hash-chained, and can be signed.
`TestAudit_RefundScenario` proves the whole path, and `audit.Verify` proves that one
edited byte fails.
