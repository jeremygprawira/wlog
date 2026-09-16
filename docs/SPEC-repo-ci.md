# Spec: repo-ci

> Module id `repo-ci` · module `github.com/jeremygprawira/wlog/tools` (own `go.mod`, never
> imported by users) · phase 10 · depends on: none. Project-wide rules in [SPEC.md](SPEC.md)
> apply. Closes REL-1 to REL-8, DOC-1, DOC-7, CLI-2, CLI-18, PIPE-23, parts of RED-11 and
> PIPE-9, and SPEC-G16, SPEC-G17, SPEC-G20.

## Objective

Make the build tell the truth before any other phase 10 work starts. Today CI fails on every
push, sub-modules build only inside `go.work`, and several plan checks run zero tests. After
this module, a green CI run means every module builds, tests, and lints on its own, at its own
Go floor.

## Behavior

### `tools` commands

`go run ./tools/cmd/<name>` from the repo root. Each command prints one line per problem as
`path:line: code: message` and exits 1 on any problem.

| Command | Does |
|---|---|
| `modules` | Lists every module in `go.work` with its path, Go floor, and dependents, as JSON. Every other command reads this list |
| `affected -base <ref>` | Lists the modules a diff touches, plus every module that depends on them |
| `requires` | Finds a sub-module that imports a sibling with no matching `require` or `replace`. It also finds sibling requires that disagree with the release version in `tools/version.txt`. Each case is a failure |
| `tidy -check` | Runs `go mod tidy -diff` with `GOWORK=off` in each module, and fails on any diff |
| `floor` | Runs `go test ./...` with `GOWORK=off` and `GOTOOLCHAIN=go<floor>` in each module. With `-libs`, it also tests the module against `testdata/floor/go.mod`, pinned to each library floor, and against a copy upgraded with `go get -u ./...`. Module directories as arguments limit the run |
| `vuln` | Runs govulncheck on a copy of each module whose build list is upgraded with `go get -u ./...`. A `require` line stays at its API floor, so this finds what users get |
| `schema` | From phase 11: tests every golden event and drain golden body against `schema/` |
| `docs` | From phase 12: builds `llms.txt` and `llms-full.txt` |
| `map` | Runs `wlog map --min-score 80` over `examples/` and the fixture apps |
| `snippets` | Extracts every fenced `go` block from README, `docs/`, `examples/`, and skill templates. It compiles each block in a scratch module. A block tagged `go run` also runs, and its output must match the next `text` block |
| `ste` | Runs the Simple English lint (`tools/ste`, copied from AminBlg/SimpleEnglish `ste_lint.py` into Go) over markdown and Go doc comments |
| `cover -min 85` | Runs coverage per root package, and fails under the minimum |
| `bench -baseline bench/baseline.txt` | Runs benchmarks with `-count=10` and compares with benchstat. It fails on a regression over 20% in time or allocations |
| `verifyplan` | Runs every `Verify:` command in `tasks/plan.md` with `-v`. A `-run` pattern that matches zero tests is a failure |
| `release -dry-run` | Prints the tag list in dependency order, the `require` updates each module needs, and the apidiff result against the last tag. Without `-dry-run`, it asks the user to type the tag list, then tags |

### Module files

- Each sub-module `go.mod` requires every sibling module it imports at the next release version,
  with no `// indirect` on a direct import. It also has a `replace` for each sibling, pointing at
  the sibling's relative path. A `GOWORK=off` build in the repo then uses the local code. A user
  ignores `replace` directives in a dependency, so the user gets the required tag. This is the
  pattern opentelemetry-go uses.
- `tools release` sets every sibling `require` to the new version in one commit, then tags the
  root module first and each other module in dependency order. `go.work` stays for local use.
- Each module's `go` line is its floor. Root: `go 1.21`. `middleware/nethttp/pattern_go123.go`
  holds the `r.Pattern` read behind `//go:build go1.23`.
- `internal/version.Version` comes from a `-ldflags` value set by `tools release`, with `"dev"`
  as the fallback. Tests compare against `version.Version`, never a literal.
- `.gitignore` covers compiled binaries by module directory name. `examples/mux/mux` leaves git.

### CI (`.github/workflows/ci.yml`)

| Job | Runs | When |
|---|---|---|
| `lint` | `golangci/golangci-lint-action` v8 or later with golangci-lint v2 built for the newest Go, then `tools ste` | every push |
| `test` | `affected`, then `go test -race` for each affected module on the newest two Go releases | every push |
| `floor` | `tools floor -libs` for each affected module | every push |
| `tidy` | `tools requires` and `tools tidy -check` | every push |
| `snippets` | `tools snippets`, plus `tools schema` once `schema/` exists in phase 11 | every push |
| `cover` | `tools cover -min 85` | every push |
| `fuzz` | every `Fuzz` target for 30s | every push |
| `fuzz-long` | every `Fuzz` target for 10 minutes, and new crash inputs open an issue | nightly |
| `bench` | `tools bench` | pull requests |
| `vuln` | `tools vuln` | nightly and on `go.mod` changes |
| `map` | `make map` over `examples/` and `cmd/wlog/testdata` fixture apps, plus `go vet -vettool` with `wlogvet` | every push |
| `integration` | Docker Compose with health probes, then `go test -tags=integration`. A service that is not ready fails the job | nightly |
| `verifyplan` | `tools verifyplan` | when `tasks/plan.md` changes |

Every job uses Node 24 action versions. `main` requires `lint`, `test`, `floor`, `tidy`,
`snippets`, `cover`, `fuzz`, and `map` to pass before merge. Setting that rule on GitHub is
ask-first.

### Repository files

`CHANGELOG.md` (Keep a Changelog format, one section per tag), `SECURITY.md` (how to report a
leak or a crash), and `CONTRIBUTING.md` (TDD rule, conformance rule, commands). `make` targets
from SPEC.md call the `tools` commands.

## Success criteria

1. On the current `main`, `tools requires` and `tools tidy -check` fail and name every
   sub-module from REL-2. After the fix, both pass.
2. `GOWORK=off go install github.com/jeremygprawira/wlog/cmd/wlog@<new tag>` works in an empty
   module. The test serves the modules that `tools release -dry-run` builds through a local
   `GOPROXY=file://` directory, so no real tag exists.
3. `tools floor` passes the root module on Go 1.21. A test file that uses a Go 1.22 feature
   without a build tag makes it fail.
4. `tools snippets` fails on the current `docs/customization.md` capture example (DOC-1), and
   passes after the fix.
5. `tools verifyplan` fails on the old plan's R3 `Verify` line, because it matches zero tests.
6. The lint job passes on a Go 1.26 or newer module.
7. A pull request that slows `BenchmarkMiddleware` by 25% fails the `bench` job.
8. `examples/mux/mux` is not tracked, and a new example binary is ignored by `.gitignore`.

## Testing

Each `tools` command has table tests over fixture repos under `tools/testdata/`. The fixtures
include a module with a missing require, an untidy module, a broken snippet, and a plan with an
empty `-run`. CI changes are proven by the jobs themselves on a branch.

## Boundaries

- **Always:** keep `tools` out of the root module's dependency graph.
- **Ask first:** changing branch protection, adding a CI secret, or tagging.
- **Never:** let `tools release` push or tag before the user types the tag list.

## Open questions

None.
