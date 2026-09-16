# Spec: cli-map

> Module id `cli-map` · module `github.com/jeremygprawira/wlog/cmd/wlog` (its own `go.mod`,
> because it needs `golang.org/x/tools`) · packages `main`, `entry`, `rules`, `score`,
> `report`, `analyzer`, `cmd/wlogvet`. Project-wide rules in [SPEC.md](SPEC.md) apply.
>
> This tool reads other people's code. It never imports the frameworks it detects. It matches
> them by package path in `go/types` information, so it stays correct without a vendor SDK.

## Objective

Give a team one command that answers "is this service observable, and what should we fix
first?". `wlog map ./...` finds every HTTP handler, checks it against a fixed rule set, prints
a deterministic 0–100 score with the top three fixes, and writes `wlog.map.json` for CI. The
same rules ship as a `go vet` analyzer, so a team can fail a build on them.

## CLI

```
wlog map [flags] <package patterns...>

  --out file        write the map to file (default wlog.map.json)
  --min-score n     exit non-zero when the score is below n
  --baseline file   exit non-zero when the score is below the score in file
  --config file     rules config (default: wlog.map.yaml, then wlog.map.json)
```

Exit codes: 0 pass, 1 gate failure (`--min-score` or `--baseline`), 2 load or config error.

## Entry-point detection

Packages load with `go/packages` and full type information. A handler is a call that registers
a function with a known framework:

| Framework | Package path | Registration |
|---|---|---|
| `nethttp` | `net/http` | `http.HandleFunc`, `http.Handle`, `(*http.ServeMux).HandleFunc`, `.Handle` |
| `mux` | `github.com/gorilla/mux` | `(*mux.Router).HandleFunc`, `.Handle` |
| `echo4` | `github.com/labstack/echo/v4` | `GET POST PUT PATCH DELETE HEAD OPTIONS Any Handle` on `*echo.Echo` or `*echo.Group` |
| `echo5` | `github.com/labstack/echo/v5` | the same method names on `*echo.Echo` or `*echo.Group` |
| `gin` | `github.com/gin-gonic/gin` | the same method names on `*gin.Engine` or `*gin.RouterGroup` |

- The route is the first string-literal argument. `mux` chains `.Methods("GET")` onto the
  registration call, so the map reads that call too and joins the method to the route.
- An `http.ServeMux` pattern may already start with a method, for example `"GET /orders/{id}"`.
  The map splits that prefix into the method and keeps the rest as the route.
- The handler is the last argument: a function literal, or an identifier that names a declared
  function. A function literal is reported under the enclosing function's name, at the
  literal's position.
- One handler with several registrations is one entry per registration.

A `Point` is one entry point: package path, function name, file base name, line, framework,
method, route, sensitive flag, and the handler's AST node.

## Rules and weights

Rules run per entry point, in this fixed order. `weight` counts toward the denominator only
when the rule applies. A rule that passes adds its weight to the score.

| Rule id | Weight | Applies to | Passes when |
|---|---|---|---|
| `middleware.coverage` | 30 | every handler | some file in the package calls `Middleware` from `middleware/nethttp`, `middleware/echo`, `middleware/echo5`, or `middleware/gin` |
| `context.set` | 20 | every handler | the handler body calls `wlog.Set`, `SetGroup`, `Append`, `Error`, `Info`, `Warn`, a `Key[T].Set`, or `audit.Do` |
| `errors.reach_wlog` | 15 | an Echo handler that returns a non-nil error | the body calls `wlog.Error` |
| `sensitive.audit` | 20, doubled to 40 when the route is sensitive | a handler whose route matches a sensitive pattern | the body calls `audit.Do` |
| `logging.no_print` | 10 | every handler | the body calls no `fmt.Print*`, `log.Print*`, `log.Fatal*`, or `log.Panic*` |
| `keys.no_denylisted` | 5 | every handler | no literal key passed to `Set`, `SetGroup`, or `Append` is denied by `redact.Default().Denies` |

`middleware.coverage` is package-level in v1: one `Middleware` call anywhere in the package
covers every handler in it. A later version can narrow this to per-router coverage.

## Sensitive routes

A route is sensitive when its lowercased text contains any default pattern:

```
pay, payment, refund, transfer, auth, login, token, password, admin, checkout, withdraw,
topup, balance
```

`wlog.map.yaml` or `wlog.map.json` may add patterns and set a minimum score:

```yaml
sensitive_routes:
  - "^/v1/payouts"
min_score: 80
```

The config file is optional. `--config` names one explicitly. The default lookup is
`wlog.map.yaml`, then `wlog.map.json`, in the working directory. A listed pattern is a
case-insensitive substring when plain, or a regular expression when it compiles as one.

## Score

For one handler, `applicable` is the sum of the weights of the rules that apply to it, and
`earned` is the sum of the weights of the rules that pass.

```
score = round(100 * sum(earned) / sum(applicable))     # 100 when nothing applies
```

`top_fixes` lists the three failed rules with the largest total lost weight, then by rule id.
Each entry is one line: rule id, total weight, and the number of handlers it fails.

## `wlog.map.json`

The map is deterministic: no timestamps, sorted keys, sorted handlers by package, file, line,
function, route, and checks in the fixed rule order above.

```json
{
  "version": 1,
  "score": 87,
  "min_score": 80,
  "pass": true,
  "handlers": [
    {
      "package": "example.com/shop",
      "function": "handleRefund",
      "file": "main.go",
      "line": 42,
      "framework": "echo4",
      "method": "POST",
      "route": "/refund",
      "sensitive": true,
      "checks": [
        {"id": "middleware.coverage", "weight": 30, "pass": true, "detail": ""}
      ]
    }
  ],
  "top_fixes": ["sensitive.audit: 40 points lost across 1 handler"]
}
```

## Baseline

`--baseline file` reads a previous `wlog.map.json`. The command fails when the current score is
strictly lower than the baseline score. The baseline's own `min_score` is ignored, so a team
can raise the floor in one place.

## Analyzer

`analyzer.Analyzer` runs the same rules inside a `go/analysis` pass and reports one diagnostic
per failed check at the handler's position. `cmd/wlogvet` is a `singlechecker` main:

```
go build -o $(go env GOPATH)/bin/wlogvet ./cmd/wlogvet
go vet -vettool=$(go env GOPATH)/bin/wlogvet ./...
```

The analyzer never writes a file and never fails a build by itself. A team decides which
diagnostics to gate on.

## Testing

- Fixture apps under `cmd/wlog/testdata/apps` cover each framework, a pass and a fail of every
  rule, and a sensitive route with and without `audit.Do`.
- `cmd/wlog/testdata/golden/*.json` pins the full map for the fixtures. Two runs must produce
  byte-identical output.
- No fixture is loaded from the network. `go test` needs no docker.

## Boundaries

- **Always:** keep the rule ids and weights stable; keep the score a pure function of the map.
- **Ask first:** adding a seventh rule, changing a weight, adding a framework.
- **Never:** import a web framework in this module, call the network, or write a timestamp into
  the map.

## Open Questions

None.
