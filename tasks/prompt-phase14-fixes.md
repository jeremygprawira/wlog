# Prompt: phase 14 review fixes

Fix every finding in `tasks/review-phase14-2026-10-02.md`. The review holds 43 high, 69 medium,
and 29 low findings. Each finding has an id, a `Where` line, and a `Fix` line. Work in the
batches below, in order. Do not stop between findings.

## First step

1. Read `CLAUDE.md`, then the whole review file, then this prompt to the end.
2. Run `git status -sb`. The tree holds two new files: the review and this prompt.
3. Commit both files as `docs(review): add the phase 14 review and the fix prompt`.
4. Add a section `Phase 14 review fixes` to `tasks/todo.md`, with one line per batch below.

## The loop for one finding

Do these steps for each finding id. One finding is one commit.

1. Read the finding. Open each file that its `Where` line names, at the lines it names.
2. Write one failing test that shows the fault. Name it `Test<Module>_<ID>_<Behavior>`, such as
   `TestNewRelic_P1_PostsToLogV1Path`.
3. Run the test with `-count=1`. Make sure that it fails for the reason in the finding.
4. Write the minimum code that passes the test. Use the `Fix` line as the plan.
5. Run `go test -race -count=1 ./...` in the module that you changed.
6. Commit as `fix(<module>): <what changed> (<ID>)`, with your own `Co-Authored-By` trailer.

Four rules apply to the loop.

- For a finding about weak tests, the new test is the fix. Break the code by hand as the
  finding says, see the new test fail, then restore the code.
- For a finding that changes behavior a spec defines, change the spec first, in the same commit.
- If a failing test is not possible because the fault does not exist, do not change the code.
  Record the id as "not reproduced", with the evidence, in the `Fixed` section.
- A finding that only names a doc or a comment needs no test. Change the text.

## The gates after each batch

Run every command below after the last commit of a batch. A red gate is the next task.

```bash
go build ./...
go test -race -count=1 ./...                      # in the root module
(cd tools && go test -race -count=1 ./...)
make lint
make tidy-check
make ste
go run ./tools/cmd/snippets
go run ./tools/cmd/pkgstate
go run ./tools/cmd/floor <dir of each own module that you changed>
```

Then do these four steps.

1. If a commit of the batch touches redaction, event storage, or emit, run `make fuzz`.
2. Run `git push`.
3. Run `gh run list --workflow ci --limit 1`, wait for the run to end, and read the result.
4. Tick the batch in `tasks/todo.md`, and add its ids to the `Fixed` section of the review.

## Rules that phase 14 broke

- Always pass `-count=1`. The test cache hid a failing `tools` test for the whole phase.
- Never report a gate as green without the output of this session.
- Run `make tidy` after each `go.mod` change, and commit the result. Never run it inside a test.
- A new or changed own module needs its line in `tools/floor-pins.txt`.
- Run `tools floor -libs` and `tools vuln` alone. Both edit `go.mod` files in place while they run.
- A golden file comes from a real system or from a hand. Never write one with the code under test.
- A test fake must answer as the real backend answers. Read `tasks/research/dest.md` and
  `tasks/research/ai.md` for the real status codes and bodies.
- Keep each fix small. Do not change code that no finding names.
- Write every doc comment, spec line, and commit message in Simple English.

## Batches

Each batch lists its findings in the order to fix them.

- A, gates: X-2, X-3, X-5, X-6, T-1, T-2, T-5. Then push, which closes X-1.
- B, pipeline and httpdrain: P-11, P-10, P-7, P-12. Then the two shared helpers that later
  batches need: the byte split with the 413 halving (P-4), and the body of a non-2xx answer (D-1).
- C, HTTP drains: P-1, P-2, P-3, P-4, P-5, P-6, P-8, P-9, P-13 to P-21.
- D, Splunk and VictoriaLogs: D-1, D-2, D-8, D-10, D-18, D-20.
- E, syslog, CloudWatch, and conformance: D-3, D-4, D-6, D-7, D-11 to D-17, D-19, D-22, D-24.
- F, shared core, OTel, and Prometheus: O-1, O-2, O-3, then O-4 to O-20.
- G, llm and the LLM SDK modules: L-8, L-9, L-1, L-19, L-22, L-18, then L-2 to L-7, L-11 to
  L-17, L-21, L-23.
- H, agent frameworks and MCP: A-1 to A-9, A-11, then A-12 to A-24.
- I, `wlog init`: I-1 to I-6, then I-7 to I-20.
- J, documents and tools: X-7, X-8, T-4, T-6. Run `make docs` and commit the result.
- K, integration: X-4, D-5, D-23, P-21, T-3. This batch needs Docker and large images.

Three notes on single findings.

- T-2, hertz: run `GOWORK=off GOOS=linux GOARCH=amd64 go build ./...` in `middleware/hertz`. It
  fails as CI does. Raise only the indirect `github.com/bytedance/sonic` pin, to the lowest
  version that builds. Name the version in the commit message.
- T-2, schema: remove `wlog.map.json` from `mapGoldens` in `tools/cmd/schema/main.go`, because
  git ignores that file.
- I-1: the end test copies the whole folder of each of the 8 recipes, runs `init --yes`, runs
  `go build`, and runs `wlog doctor`.

## Decisions

The review lists 11 decisions for the owner. Until the owner changes one, use these defaults.

1. L-9: build the spec. Add `ResponseModel`, `Status`, `FinishReasons`, `Attempts`,
   `RequestIDs`, and `Err` to `llm.Record`, and the id to `ToolCall`. `llm.Add` records one
   `calls` entry. Keep the `int` counts, and correct the spec sketch to `int`.
2. L-10: remove the body tee rule and the HTTP status from SPEC-track-g. Build no tee.
3. I-7: narrow the spec. `init` wires the HTTP frameworks, and it prints the setup line of
   each other adapter.
4. A-5: use the call kind `other`.
5. A-13: write a short hash of the session id under `rpc.mcp.session`. Make sure that
   `Redactor.Denies` does not match the new key. Update the spec table.
6. L-1 and O-10: this default needs a yes from the owner first. See "Stop only for these".
7. O-5: remove the drain counters from both modules and from the spec.
8. L-21: rename the package of `ai/goopenai` to `wloggoopenai`. The module has no tag.
9. A-11: the shared golden holds the whole event, except `error.message`, `error.type`, and
   `error.cause`.
10. L-19: add no price row. Make the prefix match accept only a date suffix or `-latest` after
    the row name. Each other suffix gives `cost_unknown`.
11. X-1: push after the gates of batch A are green.

## Stop only for these

- New exported core API. L-1 needs one function that updates a group under the event lock. The
  proposed form is `wlog.UpdateGroup(ctx context.Context, name string, update func(map[string]any))`.
  O-10 keeps `Fields()`, returns a copy, and adds it to SPEC-core-v2. Ask the owner before
  batch G, and do batch F first.
- A new dependency, a change to the default denylist or the capture defaults, or a new
  reserved event key.
- Batch K. Ask the owner before the first `docker compose` command.
- Tagging v0.9.0. The review point needs a human review first.
- A blocker that the tools at hand do not resolve.
- The end of a batch at the context limit. Commit on green and push first, then name the
  batches that remain.

## The end state

- Every gate in "The gates after each batch" is green, and the last CI run on `main` is green.
- `wlog init --yes` passes on full copies of all 8 recipe apps.
- The review file ends with a section `Fixed, <date>`. It lists each id with its commit, each
  decision with its answer, and each id that was not reproduced.
- `tasks/todo.md` shows each batch as done. The line for review point 14 stays open.
