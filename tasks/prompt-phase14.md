# Prompt: phase 14, v0.9

Continue phase 14 from tasks/todo.md, starting at 14-E-1. Work without stopping between
tasks: RED test first, minimum code, regression, one commit per task, tick the todo, push
after each green task.

Apply these skills on every task. They are not optional, and they do not expire.

## Rules of engagement

Simple English, for every word you write. This covers doc comments, README text, specs,
commit messages, and your replies. Active voice. One instruction per sentence, 20 words at
most. One description per sentence, 25 words at most. No semicolons. No em dashes. No
contractions. No Latin abbreviations, so write "for example" and not "e.g.". Use only can,
will, and must as modals. Do not use any other modal. One word means one thing
across the whole document. Never change code, commands, or paths to satisfy a style rule.
Every doc comment explains the flow, and it does not restate the signature.

Ponytail, full level. The ladder is a reflex. Stop at the first rung that holds: Does this
need to exist at all. Is it already in this codebase. Does the standard library do it. Does
a native platform feature cover it. Does an installed dependency solve it. Can it be one
line. Only then write the minimum code that works. Never add an interface with one
implementation, a factory for one product, or config for a value that never changes.
Deletion beats addition. Fewest files. Shortest working diff, but only after you understand
the problem. Mark a deliberate shortcut that has a known ceiling with a ponytail comment
that names the ceiling and the upgrade path. Leave one runnable test behind for any
non-trivial logic. Never simplify away input validation at a trust boundary, error handling
that prevents data loss, security, accessibility, or anything asked for outright.

Test driven development, without exception. Write one failing test. Watch it fail for the
right reason. Write the minimum code that passes it. Refactor. Commit on green. Never write
a batch of tests ahead of the implementation. Name tests Test<Module>_<AuditID>_<Behavior>.

Incremental implementation. Deliver thin vertical slices that build, test, and commit on
their own. A task that cannot land in one step is two tasks, and say so.

Ghostwriting rules. Comment every file, and every exported type and function. Keep the
safety gates green. Run make race and make fuzz before any commit that touches redaction,
event storage, or emit. The root module stays standard library only. A package with a
third-party import gets its own go.mod under go.work. Commit trailer: Co-Authored-By: Pi
<noreply@pi.dev>. Never commit a stray build artifact.

Build skill, auto mode. Run the task's own Verify command, then the wider regression, then
commit, then tick the todo, then push, then start the next task. Do not ask between tasks.

## Stop only for these

- A blocker you cannot resolve with the tools you have.
- An ask-first item: a new dependency, a breaking public API change beyond the ones the
  spec names, a change to the default denylist or capture defaults, a new remote, or any
  destructive action. Pushing to main is fine. Tagging a release is not.
- The end of a batch, at the context limit. Commit on green and push first, then say which
  tasks remain.

## Where the work stands

Phase 13 is complete. v0.8.0 is released and pushed with 138 tags. HEAD is 768f3e5. The tree
is clean. CI runs on main, and the lint gate is green after a fix for the deprecated AWS
resolver call in `client/aws`.

tasks/todo.md reads 147 done and 21 open. The 21 open tasks are phase 14, phase 15, and the
two review points. Phase 14 is not started. Start at 14-E-1 pipeline.PartialError.

Phase 13 landed: the 19 async adapters, the three recipes, and the fixes for the review of
2026-09-22. Every finding in that review is closed except two ceilings, which the work suite
doc names: `queue-franz` has no panic path in its per-message helper, and `queue-watermill`
exports no setter for the topic. Do not re-open them.

Two environment items stay open and need no code. The vuln scan reports a standard library
issue on Go 1.26.1 that Go 1.26.4 fixes. The repo keeps no toolchain pin on purpose, because
the low Go floors matter. The make integration target needs a Docker daemon, so it is a
manual step.

Three conventions landed in phase 13, and phase 14 uses them.

- `tools/floor-pins.txt` holds the go line and the integrated library version of every
  module. A later change that raises either one fails `tools floor`. Add a line for every
  module you add, so the spec table and the go.mod stay together.
- The work conformance suite takes a `Declaration` from a factory: the kinds it produces,
  the messaging system, whether its library reports a delivery count, and whether the
  factory can set a job attempt. A factory drives the real entry with a fake client.
- `tasks/review-phase13-2026-09-22.md` is the model for a phase review. The phase 14 review
  gets its own file, and it records the decisions, the ceilings, and the gate results.

## Session budget

16 tasks and one review point do not fit one session. Work in batches, and stop at a green
commit and a push rather than mid-edit. A sensible split:

(A) 14-E-1 pipeline.PartialError, and 14-E-2 trace-otel.
(B) 14-E-3 trace-otellog, and 14-E-4 metrics-prometheus.
(C) 14-E-5 drain-honeycomb and drain-newrelic, 14-E-6 drain-elastic, and 14-E-7 drain-splunk
    and drain-victorialogs.
(D) 14-E-8 drain-syslog, and 14-E-9 drain-cloudwatch.
(E) 14-G-1 llm additions, and 14-G-2 ai-anthropic and ai-openai.
(F) 14-G-3 ai-genai and ai-goopenai, 14-G-4 ai-langchaingo and ai-eino, and 14-G-5 ai-mcpsdk
    and ai-mcpgo.
(G) 14-G-6 Recipes: llm-agent and mcp-server, and 14-G-7 `wlog init` v2.
(H) Review point 14, v0.9.0.

Start with batch A. Track E comes first, because the destinations serve the events that
phase 13 produces. The two tracks share no files, so a later session can run them in
parallel.

## Repo mechanics that cost time

- Read order: docs/CAPABILITIES.md, then docs/SPEC.md, then docs/SPEC-track-e.md and
  docs/SPEC-track-g.md, then tasks/plan.md, then tasks/todo.md, then tasks/research/dest.md
  and tasks/research/ai.md for the dependency table with the library floors. The drain rules
  live in docs/SPEC-hardening.md.
- make ste lints docs/*.md, CLAUDE.md, README.md, and CHANGELOG.md. Run the bare make ste
  before committing any prose.
- go run ./tools/cmd/snippets compiles every fenced Go block. A block that shows an API
  shape and does not compile needs <!-- snippet:sketch --> on the line above it.
- A go.sum that lacks a /go.mod hash fails make tidy-check. Run make tidy and commit the
  result. Never run make tidy as part of a Verify command, because it also runs go work sync,
  which rewrites every module.
- make cover fails a root package that falls under 85 percent without a line in
  tools/cover-known-low.txt. A new test-only package must clear the floor, or trim the dead
  code that drags it. A test that covers new shared code belongs in that package's own test,
  or the cover gate fails.
- A new module needs its own go.mod, an entry in go.work, a line in
  tools/floor-pins.txt, and a run of make tidy and make floor. CI reads the module list from
  go.work.
- A new drain joins the drain conformance suite, and an HTTP drain joins its HTTP part.
- The release command is tools release -version v0.9.0. It prints the tag list, asks you to
  type it back, then edits each require, commits, and tags locally. It
  never pushes. Tagging is ask-first.
- tools verifyplan -only <task-id> runs a task's Verify command. A Verify command that
  matches no test is a failure. A Verify command must not change the tree.
- Never put make integration in a Verify line, because it needs Docker. Write the
  integration run as a manual note after the Verify command.
- make floor -libs runs the floor test and then an upgraded set. A break that belongs to a
  dependency goes in tools/floor-known-broken.txt as one ./dir key with a reason after a #.
- verifyplan adds -v to every go test of a compound command. A run that matches the pattern
  stays visible even after a package with no test.

## Ask first

- A new dependency. Every task in this phase adds one or two libraries. State the library
  and the version before you add it, and pin it in the module's own go.mod. The spec tables
  name each one.
- Tagging v0.9.0. The review point needs a human review first.
- Injecting any request option into a user's SDK call by default.

## Phase 14 review point

- Every Track E and G module passes its tests, suites, and floor.
- `wlog init --yes` works on every recipe app.
- Human review. Tagging v0.9.0 and the new module tags is ask-first.

## Task 14-E-1 brief

Spec row: `pipeline.PartialError` in docs/SPEC-track-e.md.

```go
type PartialError struct {
	Retry   []int  // batch indexes to send again
	Dropped []int  // batch indexes that the backend refused for good
	Reason  string // the backend's error type or code, never an event value
}

func (e *PartialError) Error() string
func (e *PartialError) Retryable() bool           // true when Retry is not empty
func (e *PartialError) RetryAfter() time.Duration // 0
```

- On a `PartialError`, the worker passes the dropped events to `OnDropped` and counts them in
  `Stats`. It queues only the `Retry` events for the next attempt, with the normal backoff.
- The wrapped drain reports `WLOG_DRAIN_DROPPED` once per reason per minute, with the count.
- The worker ignores an index outside the batch. An index in both lists counts as dropped.

Criterion 1: a `Sender` returns `PartialError{Retry: [1], Dropped: [2]}` for a batch of
three. `OnDropped` receives event 2, and the next attempt sends only event 1.

Verify: `go test -race -run 'TestPipeline_PartialError' ./pipeline`. Size S. Files:
`pipeline/partial.go`, `pipeline/pipeline.go`, `pipeline/partial_test.go`.

Read `pipeline/pipeline.go` and `pipeline/config.go` first. The worker loop, the buffer, and
the retry path are there. Add the smallest change that reads the new error type in the send
result. Keep the existing retry and drop behavior for every other error.
