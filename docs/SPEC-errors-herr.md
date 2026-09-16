# Spec: errors-herr

> Module id `errors-herr` · package `github.com/jeremygprawira/wlog/errors/herr` (`wlogherr`) ·
> own `go.mod` (`go 1.26.1`, matching herr) · depends on: `core`, `github.com/jeremygprawira/herr
> v0.1.0`. Project-wide rules in [SPEC.md](SPEC.md) apply.
> v1.2 additions to this module: [SPEC-v1.2-additions.md](SPEC-v1.2-additions.md) (the catalog bridge).

## Objective

A `wlog.ErrorExtractor` for [herr](https://github.com/jeremygprawira/herr), so `wlog.Error(ctx,
err)` gets herr's full internal detail without core knowing herr exists.

## Behaviour

```go
func Extractor(opts ...Option) wlog.ErrorExtractor

func WithPublicMessage() Option // also copy herr's public Message into ErrorInfo.Message
                                  // default: Message comes from Internal detail, not the public one
```

`Extract(err)` calls `herr.LogRecord(err)` and maps:

| herr.Record | wlog.ErrorInfo |
|---|---|
| `Code` | `Code` |
| `Kind` (stringified) | `Kind` |
| `HTTPStatus` | `Status` |
| `Internal`. With `WithPublicMessage`, an empty `Internal` uses the public message | `Message` |
| `Cause.Error()` | `Cause` |
| `Stack` | `Stack` |
| `Fields` (`[]herr.Field` → `map[string]any`) | `Attrs` |
| None (herr has no why/fix/link today) | `Why`, `Fix`, `Link` left empty |

A non-herr error still produces a usable `ErrorInfo`. herr.LogRecord's own fallback uses code
`"INTERNAL"` and wraps the error as `Cause`, which matches core's own `defaultExtractor`
shape. `wlogherr.Extractor()` is then safe as the *only* configured extractor, even in code
that sometimes returns plain errors.

## Success Criteria

1. A herr error built with `.Kind(...).Public(...).Internal(...).With(...).Wrap(cause)` maps
   every populated field above correctly.
2. herr's public `Message` never appears in `ErrorInfo.Message` unless `WithPublicMessage()` is
   set. A herr error whose internal and public messages differ proves it.
3. A plain `errors.New(...)` still produces `ErrorInfo{Code: "INTERNAL", ...}`.
4. `go list -deps` shows exactly one non-stdlib import: `github.com/jeremygprawira/herr`.

## Testing

Package `wlogherr_test`, black-box, using real `herr.New(...)`/`herr.Define(...)` values.

## Boundaries

- **Always:** never let herr's public surface leak into logs beyond what `WithPublicMessage`
  explicitly opts into. herr's own C2 guarantee (no internal leaks) works in the other
  direction and is unaffected.
- **Ask first:** bumping the pinned herr version.
- **Never:** import herr from the root module. This stays in its own `go.mod`.

## Open Questions

None.
