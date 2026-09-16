# Spec: cli-v1.3

> Module ids `cli-init`, `cli-doctor`, `cli-agents`, plus additions to `cli-map`.
> Package `github.com/jeremygprawira/wlog/cmd/wlog`. Own `go.mod`.
> Depends on `core`, `catalog`, `audit`, and the existing `cli-map`.
> Project-wide rules in [SPEC.md](SPEC.md) apply. Closes gaps 6, 7, 8, 9, and 10 in
> [evlog parity](evlog-parity.md).

## Objective

Turn `wlog` from one command into the four a team actually runs: set it up, diagnose it,
teach an agent about it, and score it. The scoring half already exists. This phase adds
the other three and deepens the rules and the report.

Everything here reads the phase 7 APIs, which is why it ships after them.

## cli-init (gap 6)

```
wlog init [--framework auto|nethttp|mux|echo|echo5|gin] [--drain stdout|axiom|loki|file]
          [--dry-run] [--dir .]
```

`init` reads the target module and picks the framework by the imports it finds. It then
writes three things. A `wlog.go` holds a `New` helper, with the service name taken from
the module path. A middleware line goes in at the router it found. A `.env.example` holds
the drain's variables. Nothing is written until the whole plan succeeds, so a failure halfway
leaves the tree as it was.

`--dry-run` prints a unified diff and writes nothing. An existing `wlog.go` is never
overwritten. `init` reports it and exits 1.

## cli-doctor (gap 7)

```
wlog doctor [--dir .] [--json]
```

`doctor` runs seven checks and prints one line each, with a pass, warn, or fail mark:

1. The module requires `github.com/jeremygprawira/wlog`, and the version is resolvable.
2. Every module in `go.work` that imports an adapter also requires it.
3. A middleware is installed on each router the entry-point scanner finds.
4. A logger reaches the middleware, rather than a fresh `wlog.New()` per request.
5. Every drain the code builds has its env vars set, or a literal value.
6. A redactor is active, and no code calls `redact.Disabled()` outside a test.
7. `wlog map` scores at or above 60.

An exit code of 0 means no fail. A warn alone still exits 0, so `doctor` in a pipeline
gates on real breakage only. `--json` prints the same result as an object per check.

## cli-agents (gap 8)

```
wlog agents [--dir .] [--skills-dir .agents/skills] [--agents-md AGENTS.md] [--dry-run]
```

`agents` writes two things. First, a wlog block in `AGENTS.md`, fenced by
`<!-- wlog:start -->` and `<!-- wlog:end -->`. A rerun then replaces only that block and
keeps the rest of the file. Second, three markdown skill files in the skills directory:

| File | What it teaches |
|---|---|
| `instrument-with-wlog.md` | Add a wide event to a handler or a job, with the field naming rules |
| `audit-a-handler.md` | Decide what needs an audit record, and write it with a catalog entry |
| `analyze-wlog-output.md` | Read an emitted event, and answer a question from a set of them |

The files are plain markdown with a short front matter block of name and description.
That format works for any tool that reads a skills folder, which keeps wlog free of one
vendor's layout.

The content is generated from the repository's own docs, so a docs change and a skill
change never drift apart. A golden test compares the written bytes.

## cli-map rules (gap 9)

Four rules join the existing six.

| Rule | Class | What it finds |
|---|---|---|
| `error-guidance` | requirement | An `ErrorInfo` reaches the event with no `why` and no `fix` |
| `swallowed-error` | requirement | An `err` assigned and then only returned or ignored, with no `wlog.Error` on that path |
| `use-catalog` | suggestion | A literal error code string that a registered catalog already holds |
| `audit-coverage` | suggestion | A handler on a write route that records no audit |

`swallowed-error` walks the control flow graph inside a function. It reports a path that
holds a live error value and reaches none of three things: a `wlog.Error`, an `Errorf`, or
a return to a caller that logs. A false positive here costs trust. The rule stays quiet on
any path it cannot fully resolve.

## cli-map report (gap 10)

```
wlog map [--all] [--entry <name>] [--json] [--min-score N] [--baseline path] [--strict]
```

- `--all` prints a matrix of every entry point against every rule.
- `--entry` prints the full detail for one entry point, with the source line per finding.
- `--json` prints the report as an object, for a dashboard or a bot.
- `--strict` fails on a per-rule regression against the baseline, not on the total alone.

The score gains three parts:

- **Entry classes.** Each entry point is read, write, or sensitive. The scanner reads the
  HTTP method and the route words, and a write or sensitive entry weighs more.
- **Grades.** A to F, from the same score, so a report reads at a glance.
- **Per-entry weighting.** A rule missed on a sensitive route costs more than the same
  miss on a health check.

Gate G6 still holds. Two runs over one tree give the same bytes, classes and grades
included.

## Success Criteria

1. `init` writes a compiling setup for each of the five frameworks, proven by building
   each generated tree in a temporary directory.
2. `init --dry-run` writes nothing, and `init` over an existing `wlog.go` exits 1.
3. `doctor` reports a fail for a missing middleware, a missing drain variable, and a
   disabled redactor, and passes on the repository's own `examples`.
4. `doctor` exits 0 with warns and no fails.
5. `agents` writes the block once, and a rerun replaces only the fenced part.
6. `agents` output matches a golden file, and the three skills exist with front matter.
7. `error-guidance` and `swallowed-error` each find their case in a fixture and stay quiet
   on the clean twin of that fixture.
8. `use-catalog` stays quiet until a registry holds the literal code, and fires then.
9. `--all`, `--entry`, and `--json` print deterministic bytes, proven by a golden test.
10. `--strict` fails on a per-rule regression whose total score did not drop.
11. A sensitive entry point scores lower than a read entry point for the same miss.

## Testing

Fixture trees under `cmd/wlog/testdata`, one per framework and one per rule, each with a
clean twin. Golden files for every printed form. The generated trees build with
`go build`, run from the test, so the scaffold can never ship broken code.

## Boundaries

- **Always:** keep gate G6. Every printed form is byte-for-byte repeatable.
- **Ask first:** changing the weights or the grade boundaries once they ship, since a
  score change breaks a team's baseline.
- **Never:** let `init` or `agents` overwrite a file it did not write. Replace only inside
  the fences, and never past them.

## Open Questions

None.
