# CLAUDE.md — wlog

Read order: [CAPABILITIES.md](docs/CAPABILITIES.md), then [SPEC.md](docs/SPEC.md), then the
module spec (`docs/SPEC-<id>.md`) for the task at hand, then [tasks/plan.md](tasks/plan.md).

## Golden rules

1. **Strict TDD, vertical slices.** Write one failing test. Write the minimum code to pass it.
   Refactor. Commit. Do not write a batch of tests ahead of the implementation.
2. **Comment every file.** A doc comment on every package, type, and exported function must
   explain the flow, not restate the signature.
3. **Safety gates never regress.** See the gate table in SPEC.md (G1 no leak, G2 race-free,
   G3 never blocks, G4 bounded memory, G5 audit integrity, G6 map determinism). Run
   `make race` and `make fuzz` before any commit that touches redaction, event storage, or emit.
4. **Root module stays stdlib-only.** A package with a third-party import gets its own `go.mod`
   under `go.work`, per the packaging rule in CAPABILITIES.md.
5. **Commit per green cycle**, with a Co-Authored-By trailer.
6. **Write in Simple English.** Follow https://github.com/AminBlg/SimpleEnglish for every doc
   comment, README, spec, and commit message: active voice, 20 words per instruction, 25 per
   description, no semicolons or em-dashes, one word per meaning. Never change code, commands,
   or paths.

## Boundaries

- **Always:** run the failing test first; keep the root `go.mod` free of third-party requirements;
  update the module's spec before changing behavior it defines.
- **Ask first:** adding any dependency; changing the default denylist, capture defaults, or
  reserved event keys; changing public API after the first tag; creating the GitHub remote,
  pushing, or tagging a release.
- **Never:** let a sink receive an event that skipped the redactor; add package-level mutable
  state; block or panic on a logging failure; make real network calls in `go test`.

## Commands

```bash
make test race fuzz bench lint tidy cover compat map
```

Single test: `go test -race -run TestName ./package`
