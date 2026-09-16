# API research, 2026-09-16

These reports hold the library and backend facts behind the phase 10 to 15 module specs. Each
fact names its source: module source from the Go proxy, a vendor doc, or an RFC. A fact marked
UNVERIFIED had no primary source, and the specs treat it that way.

| Report | Specs that use it |
|---|---|
| [http.md](http.md) | [SPEC-http-core.md](../../docs/SPEC-http-core.md), [SPEC-track-a.md](../../docs/SPEC-track-a.md) |
| [rpc.md](rpc.md) | [SPEC-track-a.md](../../docs/SPEC-track-a.md) |
| [stores.md](stores.md) | [SPEC-track-b.md](../../docs/SPEC-track-b.md) |
| [logs.md](logs.md) | [SPEC-track-c.md](../../docs/SPEC-track-c.md) |
| [async.md](async.md) | [SPEC-track-d.md](../../docs/SPEC-track-d.md), [SPEC-work.md](../../docs/SPEC-work.md) |
| [dest.md](dest.md) | [SPEC-track-e.md](../../docs/SPEC-track-e.md), the output presets in [SPEC-core-v2.md](../../docs/SPEC-core-v2.md) |
| [ai.md](ai.md) | [SPEC-track-g.md](../../docs/SPEC-track-g.md) |

The reports name probe programs under `scratchpad/research/*-work/`. Those probes were throwaway
modules, and the repo does not keep them. Two artifacts stay:

- `prototypes/sqlshape/` holds the SQL shaper prototype and its tests. The files end in `.txt`, so
  Go does not build them.
- `semconv143-attrs.tsv` lists every attribute name and stability level in the Go
  `semconv/v1.43.0` package. The OTel preset test copies it into `preset/testdata/`.
