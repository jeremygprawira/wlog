# Spec: drains-v1.4

> Module ids `drain-posthog`, `drain-betterstack`, `drain-hyperdx`, plus an AWS Lambda
> example and a best-practice guide.
> Packages `github.com/jeremygprawira/wlog/drain/posthog`, `.../betterstack`,
> `.../hyperdx`. Root module, standard library only. Depends on `core` and `pipeline`.
> Project-wide rules in [SPEC.md](SPEC.md) apply. Closes gaps 11, 13, and 14 in
> [evlog parity](evlog-parity.md).

## Objective

Three more backends, reached the same way the six v1 drains already are. Each one wraps
`internal/httpdrain`, batches through `pipeline.Wrap`, and builds from environment
variables alone. Plus the one framework example still missing, and the best-practice guide.

## Behaviour

Every drain follows the shape [SPEC-drains-v1.md](SPEC-drains-v1.md) already set.

```go
func New(opts ...Option) (wlog.Drain, error)     // reads env, then applies opts
func Must(opts ...Option) wlog.Drain             // panics on a bad setup, for main()
```

### drain-posthog

| Setting | Env var | Default |
|---|---|---|
| API key | `POSTHOG_API_KEY` | required |
| Host | `POSTHOG_HOST` | `https://us.i.posthog.com` |
| Event name | `WLOG_POSTHOG_EVENT` | `wlog_event` |

PostHog takes a batch at `/batch/`, as an object with an `api_key` and a `batch` array.
Each entry is `{event, distinct_id, properties, timestamp}`. The whole wlog event becomes
`properties`, flattened to dotted keys, since PostHog charts a flat property well and a
nested object poorly. `distinct_id` comes from `user.id`, and falls back to
`trace.request_id`. A flattened key never collides, because core's own keys already read
as paths.

### drain-betterstack

| Setting | Env var | Default |
|---|---|---|
| Source token | `BETTERSTACK_SOURCE_TOKEN` | required |
| Ingest host | `BETTERSTACK_HOST` | `https://in.logs.betterstack.com` |

Better Stack takes a JSON array of objects, with a `Bearer` token header. The event goes
over as it is, nested. `dt` carries the timestamp, and `level` and `message` map from the
event's own fields, so the product's own columns line up.

### drain-hyperdx

| Setting | Env var | Default |
|---|---|---|
| API key | `HYPERDX_API_KEY` | required |
| Endpoint | `HYPERDX_ENDPOINT` | `https://in-otel.hyperdx.io/v1/logs` |
| Service name | `HYPERDX_SERVICE` | the logger's `service.name` |

HyperDX takes OTLP over HTTP and JSON. This drain reuses the existing `drain/otlp`
encoder rather than writing a second one. It changes three things only: the endpoint, the
auth header, and the service resource attribute. A shared encoder means one place to fix
an OTLP bug.

## AWS Lambda example (gap 14)

`examples/lambda` holds a runnable handler and a small helper.

```go
func Handler(log *wlog.Logger, fn func(ctx context.Context, in Event) (Response, error)) ...
```

The helper starts one event per invocation, sets `faas.request_id`, `faas.cold_start`,
`faas.remaining_ms`, and the function name, and flushes drains before it returns. A Lambda
freezes between invocations, so an unflushed batch is lost. The helper calls
`log.Close(ctx)` on a deadline taken from the invocation's own remaining time.

This is an example module with its own `go.mod`, since it pulls in the AWS Lambda
libraries. Core stays clean.

## Best-practice guide (gap 13)

`docs/best-practices.md` covers five topics. What to put on an event, and what to leave
off. How to name a field. The right moment to open a `Detach`. How to pick a sampling
rate. What belongs in an audit record rather than a log line. It draws every rule from a
test in this repository, so the guide never states what the code does not do.

## Success Criteria

1. Each drain builds from its env vars alone, with no code options.
2. Each drain posts the shape its product documents, proven against `internal/httpfake`
   with a golden body.
3. Gate G1 holds per drain. A denied value never reaches the fake server. One leak test
   per drain.
4. Each drain returns a retryable error for a 429 and a 5xx, and a permanent one for other
   4xx, the same as the v1 drains.
5. PostHog flattens a nested event to dotted keys, and picks `distinct_id` from `user.id`
   first and `trace.request_id` second.
6. HyperDX produces the same body as `drain/otlp` for the same event, apart from the
   endpoint, the header, and the service attribute.
7. The Lambda helper flushes before returning, proven by a drain that records its `Close`
   call.
8. The Lambda example builds, and its test runs the handler through the helper.
9. The three drains import nothing outside the standard library plus `core` and
   `pipeline`.

## Testing

`internal/httpfake` for every drain, never a real network call. Golden bodies per product.
The Lambda example tests in its own module.

## Boundaries

- **Always:** reuse `internal/httpdrain` and `pipeline.Wrap`. A drain writes no retry loop
  of its own.
- **Ask first:** changing a default endpoint, since a wrong one sends a customer's data to
  the wrong region.
- **Never:** make a real network call in a test.

## Open Questions

None.
