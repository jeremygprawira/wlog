# Spec: conformance

> Module id `conformance` · packages `internal/conformance/{http,work,calls,log,drain}` · root
> module (importable by sub-modules in this repo) · phase 11 · depends on: `http-core`, `work`,
> `core-calls`, `pipeline`. Project-wide rules in [SPEC.md](SPEC.md) apply. Closes HTTP-22,
> PIPE-25, and SPEC-G20.

## Objective

Make "agnostic" a test result. Every adapter of one kind runs the same scenarios and must emit the
same normalized events. Gate G7 (one shape) is this suite passing for every adapter.

## Shared harness

<!-- snippet:sketch -->
```go
type Recorder interface {           // implemented by the suite, backed by drain-memory
	Events() []map[string]any
	Problems() []wlog.Problem
}

func Normalize(event map[string]any) map[string]any // removes values that change between runs
func Diff(want, got map[string]any) string          // readable diff for a failure message
```

`Normalize` removes `timestamp`, `event_id`, every trace and span id, `service.instance`, and
every key ending in `_ms` at any depth. It replaces the duration text inside `summary` with `{d}`,
and removes the port from `http.host`.

Each suite takes an adapter factory from the test. The factory builds a Logger with the suite's
recorder, redactor, and options, and returns something the suite can drive. Every scenario checks
three things. The normalized event must equal a golden event, and the event must be valid against
`schema/event.v1.json`. No secret from the scenario can appear anywhere in the recorded output.

## Suites and scenarios

### `http` (every HTTP router and HTTP-based RPC layer)

The adapter gives a factory that serves a fixed route table: `GET /ok`, `POST /orders/{id}`,
`GET /panic`, `GET /status/{code}`, `GET /stream`, `GET /ws`, and an unmatched path.

1. `GET /ok` gives the core fields, `http.route`, `operation` `GET /ok`, and level `info`.
2. `POST /orders/42` gives the route template, not the path, in `operation` and `http.route`.
3. An unmatched path gives route `""`, `operation` `GET unmatched`, status 404, and level `warn`.
4. `/status/503` gives level `error`. `/status/429` gives level `warn`.
5. A handler that changes the status before writing gives the final status, on the wire and in
   the event.
6. `/panic` gives 500, an error with a stack, and one event. A later middleware does not run.
7. `http.ErrAbortHandler` panics again after the event emits.
8. A valid `traceparent` sets the trace id. An invalid one generates a new trace id. A valid
   `X-Request-ID` is kept and echoed, and an invalid one is replaced.
9. `wlog.Set` in the handler reaches the event.
10. Safe default capture: allow-listed headers only, query and cookie names only, no bodies.
11. `CaptureAll()`: bodies for JSON objects, JSON arrays, and a body cut at the cap, all
    redacted by key. The handler still reads the full body.
12. Secrets in an `Authorization` header, a cookie, a query value, a JSON body, a JSON array body,
    a `Referer` URL, and a URL path never appear in output.
13. `SkipPaths` gives no event.
14. `bytes_in`, `bytes_out`, `client_ip` behind a trusted proxy and an untrusted one, and
    `user_agent`.
15. `Flusher`, `Hijacker`, and `http.ResponseController` deadlines work through the adapter.
16. An error after a committed response never writes a second body.
17. `Starter` and `Finisher` plugins run once each. A panicking one is reported,
    and the response is unchanged.
18. A framework-native error path (an Echo returned error, a Gin `c.Error`, a Fiber returned error)
    gives the same level and status as the net/http equivalent.
19. HEAD, 204, and 304 capture no body.

### `work` (messages, jobs, RPC servers, commands, functions)

The adapter gives a factory that processes one unit with a handler the suite supplies.

1. A handler returning nil gives level `info`, `outcome` `success`, `kind`, the group, and
   `operation`.
2. A handler returning an error gives level `error` and the error.
3. A panicking handler gives one event with a stack. The panic follows the adapter's documented
   policy.
4. An incoming carrier with `traceparent` links the trace. An outgoing publish or call injects a
   `traceparent` with the call's span id.
5. A message `delivery_count` above 1 appears in the group and in `summary`. A job `attempt`
   above 1 does too.
6. A secret in a message key, a body, a header, or a command argument never appears in output.
7. A slow handler never delays another unit's event.
8. `Starter` and `Finisher` plugins run once each. A panicking one is reported, and the handler
   result is unchanged.

### `calls` (outbound clients and data stores)

1. One operation gives one call record with `kind`, `system`, `operation`, `target`, `status`,
   and `duration_ms`.
2. A failed operation gives a record with `error`. The app's own error value is unchanged.
3. A call with no event on the context changes nothing, and reports nothing.
4. 60 operations keep 50 records and count all 60 in `call_stats`.
5. Parameters, bodies, and credentials never appear in output.
6. The client or driver behaves exactly as it does without the adapter, for results, errors, and
   optional interfaces.
7. Protocols with headers send `traceparent`.

### `log` (logger bridges)

1. Input: a record logged with a context that holds an event lands in `logs` with level, message,
   and attributes. A record without an event passes to the wrapped logger unchanged.
2. Input: 60 records keep 50, and count the rest in `wlog.dropped_logs`.
3. Input: the bridge's own level filter still applies.
4. Output: one finished event becomes one record with the level mapped and nested groups kept.
5. Output: user keys named `msg`, `message`, `level`, and `time` never overwrite the logger's own
   keys.
6. A secret in an attribute never appears in output.

### `drain` (every backend drain)

The core part, scenarios 1 and 4 to 7, applies to every drain. Scenario 2 applies to every drain
that writes bytes or SDK input. Scenario 3 applies to HTTP drains only. A drain spec lists any
scenario that does not apply, with the reason.

1. `New` with only the documented env vars builds the drain. A missing required var returns an
   error that names it.
2. A batch of three events gives wire output equal to a hand-written golden from the vendor docs:
   an HTTP body, syslog frames, or SDK input values.
3. The status classes behave as documented: 2xx sent, 400 and 401 dropped, 413 split, 429 and 5xx
   retried with `Retry-After`.
4. A secret under a denied key never appears in any request.
5. No credential appears in any `Problem` or `OnDropped` error.
6. `Flush` sends pending events, and `Close` sends them and stops.
7. A hung server never blocks `Send`.

## Success criteria

1. Every existing adapter (`http-std`, `http-echo`, `http-echo5`, `http-gin`, the mux example)
   passes the `http` suite in phase 11.
2. Every existing drain passes the parts of the `drain` suite that apply to it in phase 11.
5. The `work` suite passes for a fake adapter of each kind.
3. A deliberately broken fake adapter fails each scenario it breaks, with a readable diff.
4. The suite runs in under 20 seconds per adapter.

## Testing

Each suite has self-tests with a correct fake adapter and a broken one per scenario. Golden
events live in `internal/conformance/<suite>/testdata/`, written by hand from the specs.

## Boundaries

- **Always:** add a scenario here before an adapter spec relies on that behavior.
- **Never:** skip a scenario for one adapter without a written reason in that adapter's spec.

## Open questions

None.
