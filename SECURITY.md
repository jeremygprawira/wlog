# Security policy

## Which versions receive a fix

The newest release receives a fix. If the problem is severe and the fix is small,
a release older than the newest one also receives a fix.

| Version | Receives a fix |
|---|---|
| The newest release | Yes |
| A version before it | Only for a severe problem |
| Anything before v1.0.0 | No. The API changes, so a fix lands in the newest release |

## How to report a problem

Do not open a public issue for a security problem. Send a private report to
`security@wlog.dev`, or use the private report form of the repository.

Give the report these facts, so the fix starts on the first reply:

1. The version, from `go list -m github.com/jeremygprawira/wlog`.
2. The smallest program that shows the problem.
3. What an attacker gains, and what the attacker must reach first.

You receive an answer within three working days. A fix follows within thirty
days for a confirmed problem, and a release note names the fix without naming
you.

## What counts as a problem

wlog carries secrets and audit facts, so these findings count:

- **A leak**: a value under a denylisted key reaches a sink, a log line, or an
  event field.
- **A redaction bypass**: a value that the redactor must mask survives into an
  event, a captured body, or a header.
- **A crash on untrusted input**: a panic, or unbounded memory, from an event
  field that a remote party controls.
- **A broken audit chain**: a line that a reader accepts after an edit, or a
  hash mismatch that the verifier accepts.
- **A silent drop**: an event that the pipeline reports as sent while it never
  reached the sink.

## What does not count

- A value that a caller stores under a key that the caller's own denylist does
  not name. The denylist is the contract.
- A drain that a user configures toward a plain-HTTP endpoint. wlog sends what
  the configuration asks for.
- A dependency advisory without a path from wlog code. `make vuln` reports it,
  and the toolchain fix follows.

## How the gates hold

Every release runs `make race`, `make fuzz`, and `make vuln`. The race detector
covers the shared state. The fuzz targets cover the redactor and the event sinks.
The vulnerability scan covers the dependency set that a user gets.
