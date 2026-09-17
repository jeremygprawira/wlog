# Spec: AI and agents (track G)

> Phase 14 · depends on: `llm`, `core-calls`, `work`. Own-module ids: `ai-anthropic`,
> `ai-openai`, `ai-genai`, `ai-langchaingo`, `ai-goopenai`, `ai-eino`, `ai-mcpsdk`, and
> `ai-mcpgo`. Root additions to `llm`. Also `cli-init` v2 in `cmd/wlog`. Project-wide rules in
> [SPEC.md](SPEC.md) apply. Facts come from the API research of 2026-09-16, checked against each
> SDK's source and the official pricing pages.

## Objective

A model call or an MCP tool call records itself on the current event with no hand-copied token
counts. Every provider fills the same fields with the same meaning, aligned with the
OpenTelemetry GenAI conventions. No prompt, completion, tool argument, or tool result is stored
unless the user opts in.

## llm additions (root)

<!-- snippet:sketch -->
```go
type Record struct {
	Provider               string   // OTel gen_ai.provider.name: openai, anthropic, gcp.gemini, gcp.vertex_ai, ...
	Operation              string   // OTel gen_ai.operation.name: chat, generate_content, embeddings, execute_tool, ...
	RequestModel           string
	ResponseModel          string   // often a dated snapshot, such as gpt-4o-2024-08-06
	ResponseID             string
	FinishReasons          []string // the provider's own values
	Status                 string   // Responses-style status, when the provider has one
	InputTokens            int64    // every input token, cached ones included
	CacheReadInputTokens   int64    // part of InputTokens
	CacheWriteInputTokens  int64    // part of InputTokens, 5 minute or unspecified writes
	CacheWrite1hInputTokens int64   // part of InputTokens, 1 hour writes
	OutputTokens           int64
	ReasoningTokens        int64    // part of OutputTokens
	Streamed               bool
	Steps                  int      // agent or chain steps, when the framework reports them
	OutputTokensPerSecond  float64  // set by llm.Add from OutputTokens and the call duration
	TimeToFirstChunkMs     float64
	ToolCalls              []ToolCall // names and ids only
	Attempts               int
	RequestIDs             []string   // the provider's request ids, one per attempt
	Err                    error
}
```

- The event keys under `llm` are the snake_case field names, such as `request_model`,
  `cache_read_input_tokens`, and `time_to_first_chunk_ms`. `Err` becomes an `error` object with
  `code` and `message`. Phase 11 already renamed `model`, `cached_input_tokens`, and
  `finish_reason`. This track adds the keys for the new fields.
- `llm.Add(ctx, r)` appends to `llm.calls`, updates the totals, and records one `calls` entry
  with kind `llm`, system `Provider`, operation `Operation`, target `ResponseModel`, and the
  duration. So `wlog query` ranks model calls with every other call.
- These fields close PAR-27 and BET-23.
- `Prices` gains `CacheWritePerMillion` and `CacheWrite1hPerMillion`. `Cost` prices uncached
  input, cache reads, both write kinds, and output, each at its own rate.
- The OTel preset maps fields to `gen_ai.*` names from `open-telemetry/semantic-conventions-genai`,
  including `gen_ai.usage.cache_read.input_tokens` and `gen_ai.usage.cache_write.input_tokens`.
  Those names are marked development upstream. The preset pins them, and its doc comment says so.

### Token rules per provider

| Provider | `InputTokens` | `CacheReadInputTokens` | Cache writes | `ReasoningTokens` |
|---|---|---|---|---|
| Anthropic Messages | `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` | `cache_read_input_tokens` | `cache_creation.ephemeral_5m_input_tokens`, `ephemeral_1h_input_tokens` | `output_tokens_details.thinking_tokens` |
| OpenAI Chat | `prompt_tokens` | `prompt_tokens_details.cached_tokens` | `prompt_tokens_details.cache_write_tokens` | `completion_tokens_details.reasoning_tokens` |
| OpenAI Responses | `input_tokens` | `input_tokens_details.cached_tokens` | `input_tokens_details.cache_write_tokens` | `output_tokens_details.reasoning_tokens` |
| Gemini | `PromptTokenCount + ToolUsePromptTokenCount` | `CachedContentTokenCount` | none | `ThoughtsTokenCount`. `OutputTokens` is `CandidatesTokenCount + ThoughtsTokenCount` |

A test per provider proves the invariants: each cache count is at most `InputTokens`, and
`ReasoningTokens` is at most `OutputTokens`.

## LLM SDK modules

Each module has two entry styles. Typed helpers turn a response into an `llm.Record`, and stream
observers watch a stream without keeping its text. Optional HTTP middleware records attempts,
HTTP status, and request ids. No module calls an SDK method that builds full text in memory,
such as `Message.Accumulate` or `ChatCompletionAccumulator`.

| Module | SDK floor and module Go floor | Typed helpers | Stream observer | Middleware |
|---|---|---|---|---|
| `ai-anthropic` | `github.com/anthropics/anthropic-sdk-go` v1.73.0, Go 1.24 | `FromMessage(*anthropic.Message)`, `FromBetaMessage(*anthropic.BetaMessage)` | `Observe(stream)` reads `message_start`, `message_delta`, and `content_block_start` types and names only | `Middleware()` for `option.WithMiddleware`. It reads `request-id` and `X-Stainless-Retry-Count` |
| `ai-openai` | `github.com/openai/openai-go/v3` v3.61.0, Go 1.25 | `FromChatCompletion`, `FromResponse` | `ObserveChat(stream)` reads usage chunks, finish reasons, and tool names. `ObserveResponses(stream)` reads terminal events | `Middleware()`, reads `x-request-id` |
| `ai-genai` | `google.golang.org/genai` v1.71.0, Go 1.24 | `FromGenerateContent(resp, backend)` | `Observe(seq iter.Seq2[...])` re-yields each chunk, and the last non-nil usage wins | `Transport(next)` wraps the authenticated transport, never replaces it |
| `ai-goopenai` | `github.com/sashabaranov/go-openai` v1.42.1, Go 1.21 (the SDK needs 1.18) | `FromChatCompletionResponse`, `FromResponse` | `ObserveChat(stream)` | `Doer(next)` implements `HTTPDoer` |
| `ai-langchaingo` | `github.com/tmc/langchaingo` v0.1.14, Go 1.24.4 | `FromContentResponse(resp, provider, model)` | none | `Handler()` for `callbacks.Handler`. Tool and chain callbacks add `calls` |
| `ai-eino` | `github.com/cloudwego/eino` v0.9.19, Go 1.21 (the SDK needs 1.18) | none | the handler drains stream copies in a goroutine and closes them | `Handler()` built with `NewHandlerHelper`. It falls back to `Message.ResponseMeta.Usage` |

Rules:

- The OpenAI stream observer never adds `stream_options.include_usage`. `WithIncludeUsage(params)`
  sets it, as an explicit opt-in.
- The langchaingo Anthropic provider never calls the end callback in v0.1.14. So the package doc
  says to call `FromContentResponse` on the result.
- A middleware body tee has a 1 MiB cap. Past the cap it records `usage_unknown` and passes the
  body through unchanged.
- A stream observer never blocks the caller's stream, and a panic inside it is reported, never
  raised.
- `WithContent()` opts into `gen_ai.input.messages`, `gen_ai.output.messages`, and tool arguments
  and results. Those values go through the redactor.

## MCP server modules

One event per MCP request, of kind `rpc`, with `rpc.system` `mcp`.

| Field | Source |
|---|---|
| `operation` | `{rpc.service}/{method}`, such as `orders-mcp/tools/call` |
| `rpc.method` | the MCP method |
| `rpc.service` | the server name |
| `rpc.mcp.tool`, `rpc.mcp.resource_uri`, `rpc.mcp.prompt` | the tool name, resource URI, or prompt name |
| `rpc.mcp.session_id`, `rpc.mcp.protocol_version`, `rpc.mcp.client` | the session and the client name and version |
| `rpc.mcp.result` | `ok`, `tool_error`, `protocol_error`, or `input_required` |
| `rpc.status_code` | the JSON-RPC error code for a protocol error |

- A tool result with `isError` sets status class client error, so it gives level `warn`. A protocol
  error gives `error`. The codes -32700, -32600, -32601, and -32602 set status class client error,
  so they give `warn`.
- A result that asks for more input (MCP spec 2026-07-28) records `rpc.mcp.result` `input_required`,
  and holds a hash of `requestState` so round trips link up.
- Tool arguments and results are never stored unless `WithContent()` is set.

| Module | Library and floor | Hook point |
|---|---|---|
| `ai-mcpsdk` | `github.com/modelcontextprotocol/go-sdk` v1.8.0, Go 1.25 | `server.AddReceivingMiddleware(wlogmcp.Middleware(log))`. Notifications are skipped. It reads client info from `InitializeParams()`, which can be nil |
| `ai-mcpgo` | `github.com/mark3labs/mcp-go` v1.1.0, Go 1.25.5 | `server.WithHooks(wlogmcpgo.Hooks(log))`. It does not use tool middleware, which misses errors and runs twice on legacy round trips. It links before and after hooks by session and request id, in a map with a 5 minute expiry and 10,000 entries at most |

## `wlog mcp` (in `cli-mcp`)

- It ships in phase 12 with Track F, and uses the root `query` package.
- Built on `github.com/modelcontextprotocol/go-sdk` v1.8.0 with `mcp.StdioTransport`. The SDK
  serves both the 2026-07-28 protocol and the older `initialize` handshake. A server that speaks
  only the older handshake fails with a current client.
- Tools, each with a Go input type so the SDK derives the schema:
  - `events_query`: the `wlog query` filters, a source path or URL, and a limit.
  - `events_by_request_id` and `events_by_trace_id`.
  - `map_entry`: one `wlog map --entry` result.
  - `explain`: one `wlog explain` entry.
  - `redact_check`: whether a key or path is masked, built on `Redactor.Denies`. It never takes a
    value.
  - `schema_event`: the event JSON Schema.
- Every tool result is JSON text plus `structuredContent`. A tool that reads files takes paths
  only from `--root` directories set on the command line.
- Adding go-sdk to `cmd/wlog` is ask-first, and approving this spec is that ask.

## cli-init v2

- `wlog init` reads `go.mod` and matches each required module to the adapter table in
  CAPABILITIES.md. It covers routers, RPC, queues, jobs, stores, loggers, error libraries, and
  LLM SDKs.
- It writes one `wlog_setup.go` that builds the Logger with `setup.FromEnv()` and installs each
  plugin. It edits each entry point through `go/ast`.
- `--yes` accepts the whole plan. `--json` prints the plan. After writing, it runs `go build` and
  `wlog doctor`, and prints both results. (PAR-33)

## Success criteria

1. Each LLM module turns a recorded fixture response from its SDK into a `Record` that matches a
   hand-written golden, per the token table.
2. An Anthropic response with 50 input tokens and 100,000 cache reads prices at 30,150 micros for
   `claude-sonnet-4-6` ($3 input, $0.30 cache read). No provider's fixture breaks the invariants.
3. Each stream observer, fed a recorded stream, gives the same `Record` as the full response. No
   text reaches output, and the caller still reads every chunk.
4. `ai-mcpsdk` and `ai-mcpgo` give identical events for the same four fixtures: a tool call, a tool
   error, an unknown tool, and an `input_required` result.
5. `wlog mcp` passes the go-sdk client against each tool, over stdio, in a test.
6. `wlog init --yes` on the `llm-agent` and `mcp-server` recipe apps writes code that builds and
   passes `wlog doctor`.

## Testing

Fixtures are recorded SDK responses and streams under each module's `testdata/`, with secrets
removed. No test calls a real provider. Each module runs `tools floor` at its SDK floor.

## Boundaries

- **Always:** keep each SDK import inside its own module.
- **Ask first:** injecting any request option into a user's SDK call by default.
- **Never:** store prompts, completions, tool arguments, or tool results without `WithContent()`.

## Open questions

None.
