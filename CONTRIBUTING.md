# Contributing

## The rules that matter

1. **A test fails first.** Write the smallest test that shows the missing
   behavior. Run it. Watch it fail. Then write the code that passes it.
2. **One vertical slice at a time.** A slice reaches the sink: it reads an
   input, changes an event, and one test proves the result. Do not write a batch
   of tests ahead of the code.
3. **A safety gate never regresses.** The six gates live in
   [docs/SPEC.md](docs/SPEC.md). Run `make race` and `make fuzz` before a commit
   that touches redaction, event storage, or emit.
4. **Every file carries its reason.** A doc comment on every package, type, and
   exported function explains the flow. It does not restate the name.
5. **Simple English.** Follow [the skill](https://github.com/AminBlg/SimpleEnglish)
   in every doc comment, README, spec, and commit message. Active voice, short
   sentences, no semicolons and no em-dashes. `make ste` checks the markdown.
6. **The root module stays stdlib-only.** A package with a third-party import
   gets its own `go.mod` under `go.work`.
7. **One commit per green cycle.** The commit message says what changed and why.

## Ask before you do these

- Add a dependency.
- Change the default denylist, the capture defaults, or the reserved event keys.
- Change a public API after v1.0.0.
- Create the remote, push, or tag a release.

## Commands

```bash
make test          # go test ./... in every module of go.work
make race          # the same with the race detector
make floor         # every module at its oldest Go, with GOWORK=off
make lint          # go vet and golangci-lint in every module
make ste           # the Simple English lint over the markdown
make snippets      # compile every Go block in the documentation
make fuzz          # every fuzz target for FUZZTIME (default 30s)
make bench         # the benchmarks against bench/baseline.txt
make cover         # coverage per root package, under 85% fails
make map           # the map score of the examples
make release-check # the plan of the next release, and the API difference
```

One test runs like this:

```bash
go test -race -run TestName ./package
```

## The shape of a change

1. Branch.
2. Write the failing test. Run it and read the failure.
3. Write the code that passes it.
4. Run the wider checks: `make test`. If a module file changed, run
   `make floor` too.
5. Commit. The message says what changed, why, and what proves it.

## How a module joins the repository

A new module needs four things:

1. Its own `go.mod`, with a `go` line that matches the lowest Go its code and its
   dependencies allow.
2. A `require` line for every sibling module it imports, at the version in
   `tools/version.txt`, with a relative `replace` line.
3. An entry in `go.work`.
4. A package doc comment that says what the module does to an event.

`make requires` and `make tidy-check` check the first two, and CI runs both.

## How a release happens

1. Run `make release-check` and read the plan. It names the tag order, the
   require updates, and the API difference against the last tag.
2. Run `go run ./tools/cmd/release -version vX.Y.Z -dry-run=false`.
3. If the command asks for the tag list, type it there. The command changes
   nothing before that answer.
4. Push the tags with `git push origin --tags`.
5. Move the Unreleased section of [CHANGELOG.md](CHANGELOG.md) under the new
   version, and commit it.
