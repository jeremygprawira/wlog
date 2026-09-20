# AGENTS.md

wlog is a Go library for wide-event logging. One unit of work writes one event with many
fields, and every sink reads the same shape. This file is the short guide for an agent
that works in this repository.

## Read first

1. `docs/CAPABILITIES.md` for the module map.
2. `docs/SPEC.md` for the project rules.
3. The module spec, such as `docs/SPEC-core.md`.
4. `tasks/plan.md` and `tasks/todo.md` for the task at hand.

## Rules

- Write the failing test first, then the minimum code that passes it.
- Comment every file, and every exported type and function. Say what the flow does.
- Keep the root module standard library only. A package with a third-party import gets
  its own `go.mod` under `go.work`.
- Never let a sink receive an event that skipped the redactor. Never log a secret.
- Commit per green cycle, with a `Co-Authored-By` trailer.

## Commands

```bash
make test race fuzz bench lint tidy cover map ste docs
go test -race -run TestName ./package
```

## Skills

The portable skills live under `cmd/wlog/internal/templates/skills/`, one folder per
skill, with the skill in `SKILL.md` and the list in `index.md`.
