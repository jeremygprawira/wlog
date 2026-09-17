# Spec: enrich

> Module id `enrich` · package `github.com/jeremygprawira/wlog/enrich` · root module ·
> depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

Built-in `wlog.Enricher`s for context every event benefits from: host/deployment metadata, a
parsed user agent, geo from CDN headers, and a user-id lookup, evlog's enricher set, in Go.

## Behaviour

<!-- snippet:sketch -->
```go
func Host(opts ...HostOption) wlog.Enricher       // sets host.name host.pid host.pod host.namespace host.node
func Deployment(opts ...DeployOption) wlog.Enricher // sets deploy.region deploy.commit deploy.version
func UserAgent(opts ...Option) wlog.Enricher       // sets http.user_agent_parsed.{browser,os,device}
func Geo(provider string, opts ...Option) wlog.Enricher // one named provider: cloudflare,
                                                      // cloudfront, vercel, or custom
func GeoHeaders(headers map[string]string) Option      // the custom provider's header names
func User(fn func(ctx context.Context) string, opts ...Option) wlog.Enricher // sets user.id

func Overwrite(on bool) HostOption // and the equivalent on every other enricher's Option type
// default false: an enricher never overwrites a field the request/handler already set
```

`Host` reads `os.Hostname()`, `os.Getpid()`, and Kubernetes downward-API env vars (`POD_NAME`,
`POD_NAMESPACE`, `NODE_NAME`) when present. `Deployment` reads `REGION`, `GIT_COMMIT`/
`COMMIT_SHA`, and falls back to the Logger's own `service.version`. `UserAgent` parses
`http.user_agent` (set by `http-std`) with a small stdlib-only matcher recognizing Chrome, Edge,
Firefox, Safari, common HTTP clients (curl, Go-http-client), and major bots. IOS/Android/Windows/
macOS/Linux. Mobile/desktop/bot device class. `Geo` reads, in order, Cloudflare (`CF-IPCountry`
country only), CloudFront (`CloudFront-Viewer-Country/Region/City/Latitude/Longitude`), Vercel
(`X-Vercel-IP-Country/Country-Region/City/Latitude/Longitude`) headers found on
`http.request_headers`, or a caller-supplied header map via `GeoHeaders`.

## Success Criteria

1. `Host()` sets `host.name`/`host.pid` on a plain event with no HTTP context.
2. `UserAgent()` correctly classifies ≥ 30 real UA strings across the browsers/OSes/device
   classes listed above. An unrecognized UA sets only `http.user_agent_parsed.raw`.
3. `Geo()` extracts full country/region/city/lat/lon from CloudFront and Vercel headers, and
   country-only from Cloudflare's single header.
4. `Overwrite(false)` (default) never replaces a field the event already has; `Overwrite(true)`
   does.
5. `User(fn)` panicking is isolated by core's existing enricher panic-recovery, no extra
   isolation code needed in this module (reuses `wlog.Logger.runEnrichers`'s guarantee).
6. Zero imports outside the standard library.

## Testing

Table tests with real captured UA strings and header sets (fixtures under `enrich/testdata/`).
Package `enrich_test`, black-box.

## Boundaries

- **Always:** default `Overwrite(false)`.
- **Ask first:** adding a new CDN's geo headers to the built-in list vs. requiring `GeoHeaders`.
- **Never:** make a network call from an enricher (no IP-geolocation lookups, CDN headers only).

## Open Questions

None.
