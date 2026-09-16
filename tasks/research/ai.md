# Research: LLM SDK usage capture, MCP middleware, `wlog mcp` server

Verified on 2026-09-16. Every fact comes from module source downloaded with `go mod download`
(paths under `~/go/pkg/mod/...`) or from an official page fetched today, unless it is marked
**UNVERIFIED**. File:line references point into the module cache.

---

## 0. Findings that change the spec

1. **Cache semantics differ per provider, and `llm.Record` models only one of them.**
   `Prices.Cost` (llm/price.go:44-57) treats `CachedInputTokens` as a subset of `InputTokens`.
   - OpenAI (Chat and Responses) and Gemini: cached tokens are inside the input count. Map directly.
   - Anthropic: `input_tokens` **excludes** cache reads and cache writes. The adapter must set
     `InputTokens = input_tokens + cache_creation_input_tokens + cache_read_input_tokens`.
     Source: anthropic-sdk-go message.go:5214-5215. That comment gives the total input as the sum of
     `input_tokens`, `cache_creation_input_tokens`, and `cache_read_input_tokens`.
2. **Cache writes are billed above the input rate, and Record has no field for them.**
   Anthropic 5m write = 1.25x input, 1h write = 2x input. OpenAI GPT-5.6 and later bill a
   write at 1.25x input. Today `Prices.Cost` prices writes at the plain input rate, so it
   under-counts. Proposal: add `CacheWriteInputTokens int` (a subset of `InputTokens`) and
   `CacheWritePerMillion int64`. Anthropic splits writes by TTL (`Usage.CacheCreation.Ephemeral5mInputTokens`,
   `Ephemeral1hInputTokens`), so an exact price needs two write rates, or it accepts a known ceiling.
3. **`DefaultPrices` in llm/price.go has wrong rows** (verified against official pricing pages today):
   - `claude-sonnet-5` is **$2 input / $10 output / $0.20 cache hit** per MTok, not $3/$15/$0.30.
     The pricing page says the $2/$10 launch price "is now the standard price" and the planned
     September 1, 2026 rise "will not occur".
   - `claude-haiku-4.5` is not an API model id. The id is `claude-haiku-4-5` (snapshot
     `claude-haiku-4-5-20251001`). The price ($1/$5/$0.10) is correct.
   - `text-embedding-3` is not a model id. The ids are `text-embedding-3-small` ($0.02) and
     `text-embedding-3-large` ($0.13).
   - `gpt-4o` ($2.50/$1.25 cached/$10) and `gpt-4o-mini` ($0.15/$0.075/$0.60) are correct.
4. **The response model is often a dated snapshot.** OpenAI returns ids like `gpt-4o-2024-08-06`
   (the gpt-4o model page lists snapshots `gpt-4o-2024-11-20`, `gpt-4o-2024-08-06`, `gpt-4o-2024-05-13`).
   An exact-key price lookup misses these. Either price on the request model, or add an
   alias or prefix match to `Prices`.
5. **OTel GenAI conventions moved repos, and one key was renamed.** In core semconv v1.42.0
   (2026-06-12) the GenAI and MCP conventions moved to `open-telemetry/semantic-conventions-genai`.
   That repo has no tagged release. It ships from `main` with schema
   `https://opentelemetry.io/schemas/gen-ai-dev/1.42.0-dev`. The cache-write key there is
   `gen_ai.usage.cache_write.input_tokens`. The older `gen_ai.usage.cache_creation.input_tokens`
   (Go `semconv/v1.41.0`) is in the core deprecated registry. Go `go.opentelemetry.io/otel`
   v1.46.0 `semconv/v1.42.0` and `v1.43.0` removed every GenAI and MCP key (MIGRATION.md lists
   119 removed `GenAI*` declarations). The last Go package with the keys is `semconv/v1.41.0`.
   Every GenAI and MCP attribute has stability **development**.
6. **MCP spec `2026-07-28` is current, and it drops the `initialize` handshake.**
   modelcontextprotocol.io/specification/versioning says "The current protocol version is
   2026-07-28". Requests carry version, client info, and capabilities in `_meta`. Servers MUST
   implement `server/discover`. A legacy-only server **fails** with a modern client (lifecycle
   compatibility table). This decides the `wlog mcp` question: use `modelcontextprotocol/go-sdk`
   (section 9).
7. **Multi round-trip requests (SEP-2322) break "one event per tool call".** In 2026-07-28 a
   `tools/call`, `prompts/get`, or `resources/read` can return `resultType: input_required` with
   `inputRequests`. The client then retries with `inputResponses` and `requestState`. One logical
   tool call becomes several JSON-RPC requests. The middleware must mark partial results, or
   correlate them by `requestState`.

---

## 1. Normalization rule for `llm.Record` (all providers)

Invariant the adapters must hold, matching `Prices.Cost` and OTel:
`CachedInputTokens <= InputTokens`, `ReasoningTokens <= OutputTokens`, and (if added)
`CacheWriteInputTokens <= InputTokens`.

| Provider / API | Record.InputTokens | Record.CachedInputTokens | cache write (proposed field) | Record.OutputTokens | Record.ReasoningTokens |
|---|---|---|---|---|---|
| Anthropic Messages | `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` | `cache_read_input_tokens` | `cache_creation_input_tokens` (split: `cache_creation.ephemeral_5m_input_tokens`, `ephemeral_1h_input_tokens`) | `output_tokens` | `output_tokens_details.thinking_tokens` (source: "Always <= output_tokens") |
| OpenAI Chat Completions | `prompt_tokens` | `prompt_tokens_details.cached_tokens` | `prompt_tokens_details.cache_write_tokens` | `completion_tokens` | `completion_tokens_details.reasoning_tokens` |
| OpenAI Responses | `input_tokens` | `input_tokens_details.cached_tokens` | `input_tokens_details.cache_write_tokens` | `output_tokens` | `output_tokens_details.reasoning_tokens` |
| Gemini (genai) | `PromptTokenCount + ToolUsePromptTokenCount` | `CachedContentTokenCount` | none reported | `CandidatesTokenCount + ThoughtsTokenCount` | `ThoughtsTokenCount` |

Evidence for "inside vs outside":
- OpenAI: the prompt caching guide's cost function computes
  `ordinaryInputTokens = inputTokens - cachedTokens - cacheWriteTokens`. The guide also says
  that cache-write pricing "is not an additive fee". The reasoning guide says reasoning tokens
  "are billed as output tokens".
- Gemini: genai types.go:3654-3656 says the prompt count includes the cached content tokens.
  types.go:3668-3670 gives `TotalTokenCount` as the sum of
  `prompt_token_count + candidates_token_count + tool_use_prompt_token_count + thoughts_token_count`.
  So thoughts and tool-use prompt tokens sit **outside** the candidate and prompt counts. The Gemini pricing page labels output
  as "Output price (including thinking tokens)".

`Record.FinishReason`: pass the raw provider string through. Values differ:
Anthropic `end_turn|max_tokens|stop_sequence|tool_use|pause_turn|refusal|model_context_window_exceeded`
(message.go:8305-8315). OpenAI Chat `stop|length|tool_calls|content_filter|function_call`.
Gemini `STOP|MAX_TOKENS|SAFETY|RECITATION|MALFORMED_FUNCTION_CALL|...` (types.go:364-400).
OpenAI Responses has no finish reason. Use `status` (`completed|failed|in_progress|cancelled|queued|incomplete`)
plus `incomplete_details.reason`.

`Record.ToolCalls` from a model response only knows the **name**. `Duration` and `Failed` are
unknown at that point, because the caller runs the tool later.

---

## 2. OpenTelemetry GenAI mapping

Source: `open-telemetry/semantic-conventions-genai` main branch. The last commit to
model/gen-ai/registry.yaml is from 2026-09-01. Files read: model/gen-ai/registry.yaml and
model/mcp/{registry,common,spans}.yaml. All keys have `stability: development`. The repo has
no tagged release.

| llm.Record / event field | OTel attribute | Notes |
|---|---|---|
| Provider | `gen_ai.provider.name` | Enum: `openai anthropic gcp.gemini gcp.vertex_ai gcp.gen_ai aws.bedrock azure.ai.openai azure.ai.inference cohere mistral_ai deepseek groq perplexity x_ai ibm.watsonx.ai moonshot_ai`. Record's comment says `"google"`, which is not a semconv value. |
| Operation | `gen_ai.operation.name` | Enum includes `chat generate_content text_completion embeddings retrieval execute_tool invoke_agent`. Record's comment says `"embedding"`. Semconv says `embeddings`. |
| Model (request) | `gen_ai.request.model` | |
| Model (response) | `gen_ai.response.model` | Record has one Model field. Consider two. |
| (missing) response id | `gen_ai.response.id` | Anthropic `Message.ID`, OpenAI `ChatCompletion.ID` / `Response.ID`, Gemini `ResponseID`. |
| FinishReason | `gen_ai.response.finish_reasons` | type string[] |
| (missing) | `gen_ai.response.status` | new, for Responses-style status |
| InputTokens | `gen_ai.usage.input_tokens` | Includes all input token types, cached tokens too (`SHOULD`). |
| CachedInputTokens | `gen_ai.usage.cache_read.input_tokens` | A subset of `gen_ai.usage.input_tokens` (`SHOULD`). |
| (proposed) cache writes | `gen_ai.usage.cache_write.input_tokens` | A subset of `gen_ai.usage.input_tokens` (`SHOULD`). The old name `gen_ai.usage.cache_creation.input_tokens` is deprecated. |
| OutputTokens | `gen_ai.usage.output_tokens` | report the billed count |
| ReasoningTokens | `gen_ai.usage.reasoning.output_tokens` | A subset of `gen_ai.usage.output_tokens` (`SHOULD`). |
| Streamed | `gen_ai.request.stream` | boolean |
| TimeToFirstToken | `gen_ai.response.time_to_first_chunk` | type double, **seconds**. wlog emits `time_to_first_chunk_ms`. |
| ToolCall.Name | `gen_ai.tool.name` | |
| (tool call id) | `gen_ai.tool.call.id` | |
| MCP method | `mcp.method.name` | required on MCP spans. Enum ids include `tools_call`, `resources_read`, `prompts_get`, `initialize`. |
| MCP session | `mcp.session.id` | Recommended for a request inside a session. |
| MCP resource | `mcp.resource.uri` | conditionally required for resource methods |
| MCP version | `mcp.protocol.version` | |
| JSON-RPC id | `jsonrpc.request.id` | Conditionally required for a request. |
| prompt name | `gen_ai.prompt.name` | for `prompts/get` |
| errors | `error.type`, `rpc.response.status_code` | If `CallToolResult.isError` is true, set `error.type` to `tool_error`. On the server, codes `-32700 -32600 -32601 -32602` are not errors (`SHOULD NOT`). |
| content (opt-in only) | `gen_ai.tool.call.arguments`, `gen_ai.tool.call.result`, `gen_ai.input.messages`, `gen_ai.output.messages` | requirement level `opt_in`. This matches the "no content unless opted in" rule. |

MCP tool spans (mcp/common.yaml): for a tool call, set `gen_ai.operation.name` to `execute_tool`
(`SHOULD`). For any other method, leave it unset (`SHOULD NOT`).

---

## 3. github.com/anthropics/anthropic-sdk-go

- **Version** v1.73.0 (2026-09-15). No `/v2` module exists (`go mod download .../v2@latest` fails).
- **License** MIT (LICENSE: "Copyright 2023 Anthropic, PBC.", MIT text).
- **Go floor** `go 1.24` (toolchain go1.25.8).
- **Modules pulled by importing the root package** (`go list -deps`): tidwall/gjson, match,
  pretty, sjson, invopop/jsonschema, pb33f/ordered-map/v2, standard-webhooks, bahlo/generic-list-go,
  buger/jsonparser, go.yaml.in/yaml/v4, golang.org/x/sync.

### Types

```go
// message.go:5120
type Message struct {
    ID           string
    Container    Container
    Content      []ContentBlockUnion
    Model        Model            // type Model string
    Role         constant.Assistant
    StopDetails  RefusalStopDetails
    StopReason   StopReason       // string
    StopSequence string
    Type         constant.Message
    Usage        Usage
}
// message.go:12202
type Usage struct {
    CacheCreation            CacheCreation      // Ephemeral1hInputTokens, Ephemeral5mInputTokens int64
    CacheCreationInputTokens int64
    CacheReadInputTokens     int64
    InferenceGeo             string
    InputTokens              int64
    OutputTokens             int64
    OutputTokensDetails      OutputTokensDetails // ThinkingTokens int64
    ServerToolUse            ServerToolUsage     // WebFetchRequests, WebSearchRequests int64
    ServiceTier              UsageServiceTier    // "standard" | "priority" | "batch"
}
```

Content block variants (message.go:3050-3069):
`text thinking redacted_thinking tool_use server_tool_use web_search_tool_result web_fetch_tool_result code_execution_tool_result bash_code_execution_tool_result text_editor_code_execution_tool_result tool_search_tool_result container_upload`.

For a block of type `tool_use` (client tool) or `server_tool_use`, `ContentBlockUnion.Name`
holds the tool name. Server tool names (message.go:8147-8157):
`web_search web_fetch code_execution bash_code_execution text_editor_code_execution tool_search_tool_regex tool_search_tool_bm25`.

Model ids in the SDK (message.go:6918-6957):
`claude-fable-5-1 claude-mythos-5-1 claude-sonnet-5 claude-fable-5 claude-mythos-5 claude-opus-5 claude-opus-4-8 claude-opus-4-7 claude-mythos-preview claude-opus-4-6 claude-sonnet-4-6 claude-haiku-4-5 claude-haiku-4-5-20251001 claude-opus-4-5 claude-opus-4-5-20251101 claude-sonnet-4-5 claude-sonnet-4-5-20250929`.

### Middleware

```go
// option/requestoption.go:194-203
type MiddlewareNext = func(*http.Request) (*http.Response, error)
type Middleware     = func(*http.Request, MiddlewareNext) (*http.Response, error)
func WithMiddleware(middlewares ...Middleware) RequestOption
func WithHTTPClient(client HTTPClient) RequestOption
func WithResponseInto(dst **http.Response) RequestOption
```

- Middleware runs **once per attempt**. The chain is built once, then called inside the retry
  loop (internal/requestconfig/requestconfig.go:417-469). Retry count is in request header
  `X-Stainless-Retry-Count`.
- Request model and stream flag: the body is replayable. For a `*bytes.Buffer` body the SDK sets
  `req.GetBody` (requestconfig.go:396-399). Middleware can call `req.GetBody()` and decode
  `{"model","stream"}` without consuming the body.
- HTTP status: `resp.StatusCode`. Request id: header `request-id` (requestconfig.go:525 puts
  `res.Header.Get("request-id")` into `apierror.Error.RequestID`).
- Usage in middleware means reading the response body. Non-stream: read, decode, restore
  `resp.Body`. Stream (`text/event-stream`): wrap `resp.Body` with a line reader that watches
  `message_start`, `message_delta`, and `content_block_start`. Both need a size cap (gate G4).
- A per-call alternative with no body parsing: `option.WithResponseInto(&httpResp)` gives the
  final response headers after retries.

### Streaming

`NewStreaming` returns `*ssestream.Stream[MessageStreamEventUnion]` with `Next() bool`,
`Current() T`, `Err() error`, `Close() error`. `Message.Accumulate(event)` (messageutil.go:25):
- `message_start`: replaces the message. It carries id, model, and initial usage (input and cache counts).
- `message_delta`: sets `StopReason`. Usage counts are "cumulative whole-message totals, so
  it overwrites rather than adds". `OutputTokens` is always overwritten. If a count is present
  (`JSON.X.Valid()`), the input or cache count is overwritten.
- `content_block_start`: appends a block, which gives the tool name for `tool_use`.
- `Accumulate` builds full text, thinking, and tool input in memory. Do not call it in a
  content-free adapter. Read only `event.Type`, `event.Message` (start),
  `event.Usage` and `event.Delta.StopReason` (delta), and `event.ContentBlock` type and name (start).
- TTFT: time to the first `content_block_delta`. **UNVERIFIED**: which event the spec means by
  "first chunk". OTel defines it as "first chunk is received in the response stream".

### Gotchas

- Anthropic `input_tokens` excludes cache counts (section 0 item 1).
- Message has no top-level "total tokens".
- `Usage.ServiceTier` and `Usage.InferenceGeo` change price. US-only `inference_geo` is 1.1x on
  Claude 4.6 and later. Fast mode on Opus 5 and Opus 4.8 costs $10/$50.
- Beta messages (`BetaMessage`, betamessageutil.go:22) have a separate type set. An adapter
  needs a second entry point for beta calls.

---

## 4. github.com/openai/openai-go (current major is v3)

- **Module** `github.com/openai/openai-go/v3` v3.61.0 (2026-09-10). No `/v4`. v2 is at v2.7.1, v1 at v1.12.0.
- **License** Apache-2.0.
- **Go floor** `go 1.25.0`.
- **go.mod requires** azcore, azidentity, aws-sdk-go-v2, aws config, tidwall/gjson, sjson.
  Importing `openai-go/v3` or `/responses` pulls only `tidwall/{gjson,match,pretty,sjson}`
  (`go list -deps`). Azure and AWS come only from the `azure/` and `bedrock/` subpackages.

### Types

```go
// chatcompletion.go:183
type ChatCompletion struct {
    ID string; Choices []ChatCompletionChoice; Created int64; Model string
    ServiceTier ChatCompletionServiceTier; SystemFingerprint string; Usage CompletionUsage
    // + Object, Metadata, Moderation
}
type ChatCompletionChoice struct { FinishReason string; Index int64; Message ChatCompletionMessage; Logprobs ... }
type ChatCompletionMessage struct { Content, Refusal string; ToolCalls []ChatCompletionMessageToolCallUnion; ... }
type ChatCompletionMessageToolCallUnion struct {
    ID string; Type string /* "function" | "custom" */
    Function ChatCompletionMessageFunctionToolCallFunction  // .Name
    Custom   ChatCompletionMessageCustomToolCallCustom      // .Name
}
// completion.go:173
type CompletionUsage struct {
    CompletionTokens, PromptTokens, TotalTokens int64
    CompletionTokensDetails struct{ AcceptedPredictionTokens, AudioTokens, ReasoningTokens, RejectedPredictionTokens, TextTokens int64 }
    PromptTokensDetails     struct{ AudioTokens, CacheWriteTokens, CachedTokens, ImageTokens, TextTokens int64 }
}
// responses/response.go
type Response struct { ID string; Model shared.ResponsesModel; Output []ResponseOutputItemUnion; Status ResponseStatus; IncompleteDetails ResponseIncompleteDetails /* .Reason */; Error ResponseError; Usage ResponseUsage; ... }
type ResponseUsage struct {
    InputTokens int64; InputTokensDetails struct{ CacheWriteTokens, CachedTokens int64 }
    OutputTokens int64; OutputTokensDetails struct{ ReasoningTokens int64 }; TotalTokens int64
}
```

Responses tool calls: `ResponseOutputItemUnion.Type == "function_call"` (also `custom_tool_call`,
`mcp_call`, `web_search_call`, ...) with `.Name`.

### Middleware

Same Stainless shape (option/requestoption.go:93-102):
`type Middleware = func(*http.Request, MiddlewareNext) (*http.Response, error)`,
`WithMiddleware(...Middleware)`, `WithHTTPClient`, `WithResponseInto(**http.Response)`
(requestoption.go:255). Called once per attempt inside the retry loop (requestconfig.go:638-639 and the loop after it).
Request id: header `x-request-id` (API reference overview page: "`x-request-id`: Unique
identifier for this API request"). The Go SDK source does not parse it.

### Streaming

- Chat: `NewStreaming` returns `*ssestream.Stream[ChatCompletionChunk]`. `ChatCompletionChunk{ID, Choices, Model, Usage CompletionUsage /* nullable */}`.
  Usage needs the request option `StreamOptions: ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}`.
  With it, "an additional chunk will be streamed before the `data: [DONE]` message" with
  `choices: []`. All other chunks carry `usage: null` (chatcompletion.go:3365-3381). If the
  stream breaks, usage is lost.
- `ChatCompletionAccumulator.AddChunk(chunk) bool` (streamaccumulator.go:140) **adds** usage
  fields per chunk. It does not accumulate `CacheWriteTokens` (only CachedTokens and
  AudioTokens, :483-484). It holds full content in memory.
- Content-free path: if `chunk.JSON.Usage.Valid()` is true, read `chunk.Usage`. Also read
  `choice.FinishReason` and `choice.Delta.ToolCalls[i].{Index, Function.Name}`. The name appears on the first delta
  for each index.
- Responses: `NewStreaming` returns `Stream[ResponseStreamEventUnion]`. Terminal events
  `response.completed`, `response.incomplete`, and `response.failed` each carry a full
  `Response` with `Usage` (responses/response.go:5654, 11399, 8421).

### Gotchas

- An adapter must decide whether to inject `include_usage`. It changes the request, and some
  OpenAI-compatible servers can reject it (**UNVERIFIED** for specific servers).
- The response `Model` is a snapshot id (section 0 item 4).
- Long context: GPT-6 Astra and GPT-5.6 prompts over 272K input tokens cost 2x input and cache,
  and 1.5x output, "for the full request" (model pages). `Prices` cannot express this yet.

---

## 5. google.golang.org/genai

- **Version** v1.71.0 (2026-08-31). **License** Apache-2.0. **Go floor** `go 1.24`.
- **Modules pulled by the root package**: heavy. cloud.google.com/go, auth, compute/metadata,
  grpc, protobuf, genproto rpc, golang.org/x/{crypto,net,sys,text}, otel, otelhttp, s2a-go,
  gax-go, gorilla/websocket, go-cmp, logr, httpsnoop.

### Types

```go
// types.go:3726
type GenerateContentResponse struct {
    SDKHTTPResponse *HTTPResponse   // Headers http.Header; Body string
    Candidates []*Candidate         // Candidate.FinishReason FinishReason; Candidate.Content.Parts[i].FunctionCall
    CreateTime time.Time
    ModelVersion string
    PromptFeedback *GenerateContentResponsePromptFeedback
    ResponseID string
    UsageMetadata *GenerateContentResponseUsageMetadata
    ModelStatus *ModelStatus
}
type GenerateContentResponseUsageMetadata struct {
    CacheTokensDetails []*ModalityTokenCount
    CachedContentTokenCount int32
    CandidatesTokenCount int32
    CandidatesTokensDetails []*ModalityTokenCount
    PromptTokenCount int32
    PromptTokensDetails []*ModalityTokenCount
    ThoughtsTokenCount int32
    ToolUsePromptTokenCount int32
    ToolUsePromptTokensDetails []*ModalityTokenCount
    TotalTokenCount int32
    TrafficType TrafficType
}
type FunctionCall struct { ID string; Args map[string]any; Name string; PartialArgs []*PartialArg; WillContinue *bool }
func (r *GenerateContentResponse) FunctionCalls() []*FunctionCall // first candidate only; logs a warning if >1 candidate
```

Entry points (models.go:5605, 5613):
`func (m Models) GenerateContent(ctx, model string, contents []*Content, config *GenerateContentConfig) (*GenerateContentResponse, error)`
`func (m Models) GenerateContentStream(ctx, model string, contents []*Content, config *GenerateContentConfig) iter.Seq2[*GenerateContentResponse, error]`

### HTTP hook

- **No middleware option.** `HTTPOptions` has `BaseURL`, `BaseURLResourceScope`, `APIVersion`,
  `Headers`, `Timeout`, `ExtraBody`, `ExtrasRequestProvider func(map[string]any) map[string]any`,
  and `RetryOptions`. None observe responses.
- Hook point: `ClientConfig.HTTPClient *http.Client` (client.go:116-120). Wrap its `Transport`
  with an `http.RoundTripper`. Every request goes through `client.Do` inside
  `retryHTTPRequest` (api_client.go:416-419, common.go:479). The transport sees each attempt.
- Vertex AI gotcha: "For Vertex AI, this client must handle authentication appropriately." A
  wrapper must wrap the authenticated transport, not replace it.
- Response headers reach the typed value. Unary: `sdkHttpResponse.headers` (api_client.go:443-446).
  Stream: each chunk's `SDKHTTPResponse.Headers` (api_client.go:491-501).

### Streaming

`iter.Seq2` yields full `GenerateContentResponse` chunks. A wrapper can re-yield and time the
first chunk. `Chat.SendMessageStream` does not merge usage (chats.go:226-260). **UNVERIFIED**:
whether each chunk's `UsageMetadata` is cumulative. The safe rule is "last non-nil
UsageMetadata wins", never sum.

### Gotchas

- Thoughts and tool-use prompt tokens are outside `CandidatesTokenCount` and `PromptTokenCount` (section 1).
- `int32` counts.
- Use `ModelVersion` for the response model.
- Provider name is `gcp.gemini` (Gemini API) or `gcp.vertex_ai` (Vertex). `ClientConfig.Backend` tells them apart.
- Operation name is `generate_content`.

---

## 6. github.com/sashabaranov/go-openai

- **Version** v1.42.1 (2026-09-11). **License** Apache-2.0. **Go floor** `go 1.18`.
  **Stdlib-only** (`go list -deps` shows no third-party module). README: "An unofficial Go client".

```go
// common.go
type Usage struct {
    PromptTokens, CompletionTokens, TotalTokens int
    PromptTokensDetails     *PromptTokensDetails     // AudioTokens, CachedTokens int  (no cache_write)
    CompletionTokensDetails *CompletionTokensDetails // AudioTokens, ReasoningTokens, AcceptedPredictionTokens, RejectedPredictionTokens int
}
type ChatCompletionResponse struct { ID, Object string; Created int64; Model string; Choices []ChatCompletionChoice; Usage Usage; SystemFingerprint string; ServiceTier ServiceTier; httpHeader }
type ChatCompletionStreamResponse struct { ID, Object string; Created int64; Model string; Choices []ChatCompletionStreamChoice; ...; Usage *Usage }
type StreamOptions struct { IncludeUsage bool `json:"include_usage,omitempty"` }
// Responses API (response.go:239)
type ResponseUsage struct { InputTokens int; InputTokensDetails *ResponseInputTokensDetails /* CachedTokens, CacheWriteTokens */; OutputTokens int; OutputTokensDetails *ResponseOutputTokensDetails /* ReasoningTokens */; TotalTokens int }
// config.go
type ClientConfig struct { BaseURL, OrgID string; APIType APIType; APIVersion, AssistantVersion string; AzureModelMapperFunc func(string) string; HTTPClient HTTPDoer; EmptyMessagesLimit uint }
type HTTPDoer interface { Do(req *http.Request) (*http.Response, error) }
```

- Hook: `ClientConfig.HTTPClient` accepts any `HTTPDoer`. There is no middleware type.
- Headers: `resp.Header()` and `GetRateLimitHeaders()` come from the embedded `httpHeader` (client.go:30-38).
- Streaming: `CreateChatCompletionStream` returns a stream. `Recv()` returns `io.EOF` at the end.
  Usage needs `StreamOptions{IncludeUsage: true}`. The chunk's `Usage` is a pointer.
- Gotchas: detail structs are pointers (nil checks). `CachedTokens` exists, but Chat has no `CacheWriteTokens`.

---

## 7. github.com/tmc/langchaingo

- **Version** v0.1.14 (2025-10-20, about 11 months without a release). **License** MIT.
  **Go floor** `go 1.24.4`. go.mod has about 300 require lines. Importing `callbacks` pulls
  dlclark/regexp2, google/uuid, pkoukk/tiktoken-go.

```go
// callbacks/callbacks.go
type Handler interface {
    HandleText(ctx context.Context, text string)
    HandleLLMStart(ctx context.Context, prompts []string)
    HandleLLMGenerateContentStart(ctx context.Context, ms []llms.MessageContent)
    HandleLLMGenerateContentEnd(ctx context.Context, res *llms.ContentResponse)
    HandleLLMError(ctx context.Context, err error)
    HandleChainStart(ctx context.Context, inputs map[string]any)
    HandleChainEnd(ctx context.Context, outputs map[string]any)
    HandleChainError(ctx context.Context, err error)
    HandleToolStart(ctx context.Context, input string)
    HandleToolEnd(ctx context.Context, output string)
    HandleToolError(ctx context.Context, err error)
    HandleAgentAction(ctx context.Context, action schema.AgentAction)
    HandleAgentFinish(ctx context.Context, finish schema.AgentFinish)
    HandleRetrieverStart(ctx context.Context, query string)
    HandleRetrieverEnd(ctx context.Context, query string, documents []schema.Document)
    HandleStreamingFunc(ctx context.Context, chunk []byte)
}
// embed callbacks.SimpleHandler (callbacks/simple.go:11) for no-op defaults
type ContentResponse struct { Choices []*ContentChoice }
type ContentChoice struct { Content, StopReason string; GenerationInfo map[string]any; FuncCall *FunctionCall; ToolCalls []ToolCall; ReasoningContent string }
```

GenerationInfo token keys, per provider (source file:line):

| Provider | input | output | cached | reasoning | value type |
|---|---|---|---|---|---|
| openai (llms/openai/openaillm.go:342-354) | `PromptTokens` | `CompletionTokens` | `PromptCachedTokens` | `ReasoningTokens`, `CompletionReasoningTokens`, `ThinkingTokens` | int |
| anthropic (llms/anthropic/anthropicllm.go:189-192) | `InputTokens` (excludes cache) | `OutputTokens` | `CacheReadInputTokens`, `CacheCreationInputTokens` | none | int |
| googleai (llms/googleai/googleai.go:174-193) | `input_tokens`, `PromptTokens` | `output_tokens`, `CompletionTokens` | `CachedTokens`, `CacheReadInputTokens`, `NonCachedInputTokens` | `ThinkingTokens` is hard-coded 0 | int32 |
| vertex (llms/googleai/vertex/vertex.go:165-166) | `input_tokens` | `output_tokens` | none | none | int32 |
| bedrock (bedrockclient provider_*.go) | `input_tokens` | `output_tokens` | none | none | int |
| ollama (llms/ollama/ollamallm.go:210-227) | `PromptTokens` | `CompletionTokens` | `CachedTokens` | `ThinkingTokens` = 0 | int |

Gotchas:
- **The anthropic provider never calls `HandleLLMGenerateContentEnd` in v0.1.14.** It calls
  only `...Start` and `HandleLLMError` (anthropicllm.go:85, 122, 163). Usage must come from the
  returned `ContentResponse`, not from a callback.
- Neither the callback nor GenerationInfo carries the model name or the response id.
- Numeric types differ (int vs int32). Parse with a type switch.
- googleai does not add thoughts to output (it maps `CandidatesTokenCount` only).
- For a stream, the openai client injects `StreamOptions{IncludeUsage: true}`
  (llms/openai/internal/openaiclient/chat.go:467-468).
- Tool names: `ContentChoice.ToolCalls[i].FunctionCall.Name`.

---

## 8. github.com/cloudwego/eino

- **Version** v0.9.19 (2026-09-01). **License** Apache-2.0 (file LICENSE-APACHE). **Go floor**
  `go 1.18`. Importing `callbacks` pulls bytedance/sonic, gonja, logrus, go-toml, x/exp, yaml,
  and about 20 more modules. Provider models live in `github.com/cloudwego/eino-ext` (component
  model/openai latest v0.1.13).

```go
// internal/callbacks/interface.go:38, aliased as callbacks.Handler
type Handler interface {
    OnStart(ctx context.Context, info *RunInfo, input CallbackInput) context.Context
    OnEnd(ctx context.Context, info *RunInfo, output CallbackOutput) context.Context
    OnError(ctx context.Context, info *RunInfo, err error) context.Context
    OnStartWithStreamInput(ctx context.Context, info *RunInfo, input *schema.StreamReader[CallbackInput]) context.Context
    OnEndWithStreamOutput(ctx context.Context, info *RunInfo, output *schema.StreamReader[CallbackOutput]) context.Context
}
type RunInfo struct { Name string; Type string; Component components.Component /* "ChatModel", "Tool", ... */ }
func AppendGlobalHandlers(handlers ...Handler) // NOT thread-safe, call once at init

// components/model/callback_extra.go
type CallbackOutput struct { Message *schema.Message; Config *Config /* .Model */; TokenUsage *TokenUsage; Extra map[string]any }
type TokenUsage struct { PromptTokens int; PromptTokenDetails PromptTokenDetails /* CachedTokens int */; CompletionTokens, TotalTokens int; CompletionTokensDetails CompletionTokensDetails /* ReasoningTokens int */ }
func ConvCallbackOutput(src callbacks.CallbackOutput) *CallbackOutput // nil for unknown types
// schema/message.go:448
type ResponseMeta struct { FinishReason string; Usage *TokenUsage; LogProbs *LogProbs }

// utils/callbacks/template.go:524 typed helper
type ModelCallbackHandler struct {
    OnStart               func(ctx, *callbacks.RunInfo, *model.CallbackInput) context.Context
    OnEnd                 func(ctx, *callbacks.RunInfo, *model.CallbackOutput) context.Context
    OnEndWithStreamOutput func(ctx, *callbacks.RunInfo, *schema.StreamReader[*model.CallbackOutput]) context.Context
    OnError               func(ctx, *callbacks.RunInfo, error) context.Context
}
// NewHandlerHelper().ChatModel(h).Tool(th).Handler()
```

Gotchas:
- **Stream copies MUST be closed** by the handler: "failure causes pipeline goroutine leak"
  (callbacks/doc.go:64-68, 112-114). Drain in a goroutine so the pipeline is never blocked (gate G3).
- When a graph node injects the callback, output is a bare `*schema.Message`, not
  `*model.CallbackOutput` (ConvCallbackOutput:110-113). Then `TokenUsage` is nil. Fall back to
  `Message.ResponseMeta.Usage`.
- Cached token semantics depend on the eino-ext provider implementation. **UNVERIFIED** per provider.
- Tool callbacks: `RunInfo.Name` is `task.name` (compose/tool_node.go:896-900), the tool name.
  The tool call id is in ctx via an unexported `toolCallInfo`.
- Handlers return ctx. OnStart can stash a start time in ctx for OnEnd.

---

## 9. MCP: github.com/modelcontextprotocol/go-sdk

- **Version** v1.8.0 (2026-09-04). Stable v1 line. No `/v2`. README compatibility table: v1.7.0+
  supports spec `2026-07-28, 2025-11-25*, 2025-06-18, 2025-03-26, 2024-11-05` (* client-side
  OAuth experimental). Roots, sampling, and logging are deprecated as of 2026-07-28 (SEP-2577).
- **License** Apache-2.0, with a note: "undergoing a licensing transition from the MIT License to
  the Apache License". Contributions without relicensing consent stay MIT.
- **Go floor** `go 1.25.0`. The `mcp` package pulls google/jsonschema-go, segmentio/encoding and
  asm, yosida95/uritemplate/v3, golang.org/x/{oauth2,sync,sys,time}. (go.mod also requires
  golang-jwt and x/tools. cmd/wlog already requires x/tools v0.47.0, and MVS picks the higher version.)

### Middleware API

```go
// mcp/shared.go:115,130
type MethodHandler func(ctx context.Context, method string, req Request) (result Result, err error)
type Middleware func(MethodHandler) MethodHandler
// mcp/server.go:1845,1860
func (s *Server) AddSendingMiddleware(middleware ...Middleware)
func (s *Server) AddReceivingMiddleware(middleware ...Middleware) // m1(m2(m3(handler))), first runs first

type Request interface { GetSession() Session; GetParams() Params; GetExtra() *RequestExtra }
type ServerRequest[P Params] struct { Session *ServerSession; Params P; Extra *RequestExtra }
type RequestExtra struct { TokenInfo *auth.TokenInfo; Header http.Header; CloseSSEStream func(CloseSSEStreamArgs) }
// mcp/requests.go
type CallToolRequest     = ServerRequest[*CallToolParamsRaw]  // Params.Name string, Params.Arguments json.RawMessage
type ReadResourceRequest = ServerRequest[*ReadResourceParams]  // Params.URI string
type GetPromptRequest    = ServerRequest[*GetPromptParams]     // Params.Name string, Params.Arguments map[string]string
// all three params also carry InputResponses InputResponseMap, RequestState string (MRTR)

type CallToolResult struct { Meta; Content []Content; StructuredContent any; IsError bool; InputRequests InputRequestMap; RequestState string /* + unexported resultType, err */ }
func (r *CallToolResult) GetError() error  // the Go error passed to SetError; nil on clients

func (ss *ServerSession) ID() string                       // "" when the transport has no session id (stdio)
func (ss *ServerSession) InitializeParams() *InitializeParams // .ClientInfo *Implementation{Name, Title, Version, ...}, .ProtocolVersion
```

Middleware sketch for one event per call:

```go
server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
    return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
        start := time.Now()
        res, err := next(ctx, method, req)
        switch r := req.(type) {
        case *mcp.CallToolRequest:     // r.Params.Name
        case *mcp.ReadResourceRequest: // r.Params.URI
        case *mcp.GetPromptRequest:    // r.Params.Name
        }
        // err != nil  -> protocol error; errors.As(err, new(*jsonrpc.Error)) gives .Code
        // res.(*mcp.CallToolResult).IsError -> tool error; .GetError() gives the Go error
        return res, err
    }
})
```

### Error semantics (source-verified)

- `ToolHandler` (raw, server.AddTool): "If ToolHandler returns an error, it is treated as a
  protocol error" (tool.go:27-29).
- `ToolHandlerFor[In, Out]` (generic `mcp.AddTool`): an error "is treated as a tool error ...
  packed into CallToolResult.Content, with IsError set" (tool.go:49-51). `SetError` keeps the Go
  error for `GetError()` (protocol.go:332-347).
- Unknown tool: `&jsonrpc.Error{Code: jsonrpc.CodeInvalidParams (-32602), Message: "unknown tool %q"}` (server.go:1005-1012).
- Codes (jsonrpc/jsonrpc.go, mcp/shared.go, mcp/resource.go):
  - -32700 parse error, -32600 invalid request, -32601 method not found.
  - -32602 invalid params, -32603 internal error, -32002 resource not found.
  - -32020 header mismatch, -32021 missing required client capabilities.
  - -32022 unsupported protocol version, -32042 URL elicitation required.
  - `jsonrpc.Error = jsonrpc2.WireError`.

### Gotchas

- **JSON-RPC request id is not exposed.** It sits in ctx under the unexported `idContextKey{}`
  (server.go:~2008). Emit `jsonrpc.request.id` only with a hand-rolled transport wrapper, or skip it.
- **Client info on 2026-07-28**: there is no initialize. The SDK fills `InitializeParams` from the
  first request's `_meta` (server.go:1987-1993, keys `io.modelcontextprotocol/clientInfo`,
  `.../protocolVersion`, `.../clientCapabilities`). Read `InitializeParams()` per call. It can be nil early.
- On 2026-07-28 these methods return -32601 "not supported in the new protocol":
  `initialize`, `ping`, `notifications/initialized`, `notifications/roots/list_changed`,
  `logging/setLevel`, `resources/subscribe`, `resources/unsubscribe` (server.go:1968-1976).
- **MRTR ordering.** `NewServer` installs `serverMultiRoundTripMiddleware` first (server.go:273).
  User middleware added later wraps it on the outside. For legacy clients the SDK resolves input
  requests and re-invokes the handler inside, so user middleware sees **one** logical call. For
  2026-07-28 clients each round trip is a separate request. A result with non-nil
  `InputRequests` is partial. Correlate with `Params.RequestState`.
- Calls run asynchronously (`jsonrpc2.Async`) except `initialize`. Middleware must be race-free.
- Notifications also pass through receiving middleware, with a nil result. Filter by method.
- `DefaultMaxLineLength = 16 MiB` for stdio frames (transport.go:124).

---

## 10. MCP: github.com/mark3labs/mcp-go

- **Version** v1.1.0 (2026-09-15). **License** MIT. The file says "Copyright (c) 2024 Anthropic, PBC".
  **Go floor** `go 1.25.5`. The `server` package pulls google/jsonschema-go, google/uuid,
  santhosh-tekuri/jsonschema/v6, spf13/cast, yosida95/uritemplate/v3, golang.org/x/text.
  (go.mod lists testify and go-internal as direct requires.)
- README: "MCP Go is under active development ... some advanced capabilities are still in progress."
- Protocol: `LATEST_PROTOCOL_VERSION = "2026-07-28"`, `LATEST_LEGACY_PROTOCOL_VERSION = "2025-11-25"` (mcp/version.go).

```go
// server/hooks.go
type BeforeAnyHookFunc   func(ctx context.Context, id any, method mcp.MCPMethod, message any)
type OnSuccessHookFunc   func(ctx context.Context, id any, method mcp.MCPMethod, message any, result any)
type OnErrorHookFunc     func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error)
type OnBeforeCallToolFunc     func(ctx context.Context, id any, message *mcp.CallToolRequest)
type OnAfterCallToolFunc      func(ctx context.Context, id any, message *mcp.CallToolRequest, result any)
type OnBeforeReadResourceFunc func(ctx context.Context, id any, message *mcp.ReadResourceRequest)
type OnAfterReadResourceFunc  func(ctx context.Context, id any, message *mcp.ReadResourceRequest, result *mcp.ReadResourceResult)
type OnBeforeGetPromptFunc    func(ctx context.Context, id any, message *mcp.GetPromptRequest)
type OnAfterGetPromptFunc     func(ctx context.Context, id any, message *mcp.GetPromptRequest, result *mcp.GetPromptResult)
func (c *Hooks) AddBeforeCallTool(OnBeforeCallToolFunc); AddAfterCallTool; AddOnError; AddBeforeAny; AddOnSuccess
func (c *Hooks) AddBeforeReadResource / AddAfterReadResource / AddBeforeGetPrompt / AddAfterGetPrompt / AddBeforeDiscover ...
func WithHooks(hooks *Hooks) ServerOption          // sets s.hooks, one Hooks value per server

// server/server.go
type ToolHandlerFunc func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
type ToolHandlerMiddleware func(ToolHandlerFunc) ToolHandlerFunc
func WithToolHandlerMiddleware(ToolHandlerMiddleware) ServerOption
func (s *MCPServer) Use(mw ...ToolHandlerMiddleware)
func WithResourceHandlerMiddleware(ResourceHandlerMiddleware) ServerOption
func WithPromptHandlerMiddleware(PromptHandlerMiddleware) ServerOption
type CallToolParams struct { Name string; Arguments any; Meta *Meta; Task *TaskParams; MultiRoundTripParams; RawArguments json.RawMessage }
func ClientSessionFromContext(ctx context.Context) ClientSession // assert SessionWithClientInfo for GetClientInfo()
```

Flow for `tools/call` (server/request_handler.go:503-528): `beforeCallTool` runs, then
`handleToolCall`. On error: `onError(ctx, id, method, &request, err)`, then a JSON-RPC error.
Otherwise: `afterCallTool`, which also fires `OnSuccess`.

Gotchas:
- **A tool handler error becomes a protocol error** (`mcp.INTERNAL_ERROR`, server.go:2139-2145),
  not an `IsError` result. Only a result with `IsError: true` makes a tool error.
- **Tool middleware misses** unknown tools (`ErrToolNotFound`), input schema failures
  (returned before middleware, server.go:2122-2126), and output schema failures. Hooks see all of these.
- **Tool middleware runs again per MRTR round trip for legacy clients** (`resolveMultiRoundTrip`
  re-invokes `finalHandler`, server.go:2152-2158). A middleware-based event fires twice.
  Prefer hooks for one event per request.
- Before and after hooks get no shared mutable ctx. They must correlate by `id any` (plus
  session), for example a `sync.Map` keyed by session id and request id, and delete in both
  after and error. If the after hook never fires, the entry leaks (gate G4 risk).
- The `id` is available (unlike go-sdk), so `jsonrpc.request.id` can be emitted.
- Task-augmented tool calls return `CreateTaskResult` immediately (`handleTaskAugmentedToolCall`).
  The after hook result type is `any` for this reason.

---

## 11. `wlog mcp` stdio server: go-sdk vs hand-rolled JSON-RPC

What a spec-conformant stdio server needs today (spec 2026-07-28, fetched):
- Framing: newline-delimited JSON-RPC 2.0, UTF-8. Messages "MUST NOT contain embedded newlines".
  Nothing but MCP messages on stdout. Logs can go to stderr (`MAY`).
- Modern (2026-07-28): no handshake. Each request carries `_meta` with
  `io.modelcontextprotocol/protocolVersion`, `.../clientInfo`, `.../clientCapabilities`.
  Server MUST implement `server/discover`. An unsupported version MUST get error
  -32022 with `data: {supported, requested}`. Responses carry `io.modelcontextprotocol/serverInfo` (`SHOULD`).
- Legacy (2025-11-25 and earlier): `initialize` request, then result with `protocolVersion`,
  `capabilities`, `serverInfo`, then `notifications/initialized`, `ping`.
- Compatibility table: Modern client with Legacy server **fails**. Modern client with Dual-era
  server works. Legacy client with Dual-era server works. On stdio, clients send
  `server/discover` first (`SHOULD`).
- Tools: `tools/list` (with pagination cursor) and `tools/call`, returning `content[]`,
  `structuredContent`, and `isError`.

| | go-sdk v1.8.0 | hand-rolled stdlib |
|---|---|---|
| Dual-era (modern `_meta` + legacy `initialize`) | built in, tested against conformance suite (`conformance/` dir) | must write both eras, version negotiation, `server/discover`, -32022 data |
| Input schema from Go types | `mcp.AddTool[In, Out]` infers the schema and verifies input against it | hand-written JSON Schema, no input verification |
| MRTR, cancellation, progress, pagination | built in | must write, or declare unsupported |
| Future spec revisions | SDK upgrade | re-read spec each revision (3 lifecycle changes since 2024) |
| Deps added to `cmd/wlog` module | jsonschema-go, segmentio/encoding+asm, uritemplate, x/oauth2, x/sync, x/sys, x/time (x/tools already present) | none |
| Go floor | 1.25.0 (cmd/wlog is on 1.26.1, fine) | none |
| Root module rule (stdlib-only) | not affected: `cmd/wlog` already has its own go.mod under go.work | not affected |
| Code size | about 30 lines of wiring | about 400-800 lines plus tests (estimate, UNVERIFIED) |

**Recommendation: build `wlog mcp` on `github.com/modelcontextprotocol/go-sdk` v1.8.0**, using
`server.Run(ctx, &mcp.StdioTransport{})`. It lives in `cmd/wlog`, which already carries
third-party deps, so the root stdlib-only rule holds. Hand-rolling is only cheap for the legacy
handshake, and a legacy-only server fails with modern clients. Dual-era by hand means copying what
the SDK already tests. Adding the dependency needs the "Ask first" approval in CLAUDE.md.

For the MCP **middleware module** (wide event per call), support go-sdk first (official, stable
v1, one middleware sees every method and error kind). Add mcp-go through `Hooks` (not tool
middleware), because tool middleware misses errors and double-fires on legacy MRTR.

---

## 12. Pricing (official pages, fetched 2026-09-16)

### Anthropic (platform.claude.com/docs/en/about-claude/pricing), USD per MTok

| Model (API id) | Input | 5m cache write | 1h cache write | Cache hit | Output |
|---|---|---|---|---|---|
| Claude Fable 5.1 (`claude-fable-5-1`) | 10 | 12.50 | 20 | 0.25 (0.025x) | 50 |
| Claude Mythos 5.1 (`claude-mythos-5-1`, limited) | 10 | 12.50 | 20 | 0.25 (0.025x) | 50 |
| Claude Fable 5 (`claude-fable-5`) | 10 | 12.50 | 20 | 1 | 50 |
| Claude Mythos 5 (`claude-mythos-5`, limited) | 10 | 12.50 | 20 | 1 | 50 |
| Claude Opus 5 (`claude-opus-5`) | 5 | 6.25 | 10 | 0.50 | 25 |
| Claude Opus 4.8 (`claude-opus-4-8`) | 5 | 6.25 | 10 | 0.50 | 25 |
| Claude Opus 4.7 / 4.6 / 4.5 | 5 | 6.25 | 10 | 0.50 | 25 |
| Claude Sonnet 5 (`claude-sonnet-5`) | 2 | 2.50 | 4 | 0.20 | 10 |
| Claude Sonnet 4.6 / 4.5 | 3 | 3.75 | 6 | 0.30 | 15 |
| Claude Haiku 4.5 (`claude-haiku-4-5`) | 1 | 1.25 | 2 | 0.10 | 5 |

Modifiers: Batch API 50% off input and output. Fast mode (Opus 5 and Opus 4.8, first-party only)
costs $10 input / $50 output. `inference_geo: "us"` is 1.1x on all categories for Claude 4.6
and later. "Claude 4.6 and later models ... include the full 1M token context window at standard
pricing". Multipliers stack. Claude 4.7 and later use a tokenizer producing "approximately 30%
more tokens for the same text". Web search costs $10 per 1,000 searches. The API ids come from
the SDK constants (section 3). The pricing page uses display names.

### OpenAI (developers.openai.com/api/docs/pricing.md; openai.com/api/pricing returned HTTP 403), USD per MTok, Standard tier, short context

| Model | Input | Cached input | Cache write | Output |
|---|---|---|---|---|
| gpt-6-astra (flagship per models page: "our flagship model for complex reasoning and coding") | 10.00 | 1.00 | 12.50 | 50.00 |
| gpt-5.6-sol | 4.00 | 0.40 | 5.00 | 20.00 |
| gpt-5.6-terra | 2.00 | 0.20 | 2.50 | 12.00 |
| gpt-5.6-luna | 0.20 | 0.02 | 0.25 | 1.20 |
| gpt-5.5 | 5.00 | 0.50 | - | 30.00 |
| gpt-5.4 | 2.50 | 0.25 | - | 15.00 |
| gpt-4.1 | 2.00 | 0.50 | - | 8.00 |
| gpt-4o | 2.50 | 1.25 | - | 10.00 |
| gpt-4o-mini | 0.15 | 0.075 | - | 0.60 |
| text-embedding-3-small | 0.02 | - | - | - |
| text-embedding-3-large | 0.13 | - | - | - |

Long context (over 272K input tokens) for gpt-6-astra: $20 / $2 / $25 / $75 ("2x input and cache
rates and 1.5x output for the full request"). gpt-5.6-terra doubles input and multiplies output
by 1.5x. Batch is 50% off. Regional processing adds 10% for models released on or after
2026-03-05. "GPT-5.6 Sol's promotional pricing is available at least through November 21, 2026."
Priority was renamed Fast mode on 2026-07-30 (`service_tier: "priority"` or `"fast"`).

Gemini prices were not requested and were not collected.

---

## 13. Proposed adapter shape (one line each)

- Two entry styles per SDK module. (a) Typed helpers:
  `FromMessage(*anthropic.Message) llm.Record`, `FromChatCompletion`, `FromResponse`,
  `FromGenerateContent`, plus stream wrappers that observe events without keeping text.
  (b) Optional HTTP middleware (Stainless `option.Middleware`, or an `http.RoundTripper` for
  genai and go-openai) for attempts, status, request id, and retries.
- Content stays out by default. Opt-in maps to the OTel `opt_in` attributes and must pass the
  redactor (gate G1).
- Stream wrappers must not block the caller's stream (gate G3). Any body tee needs a byte cap (gate G4).

---

## 14. UNVERIFIED items

- Whether Gemini `UsageMetadata` in streamed chunks is cumulative (use last non-nil).
- The exact event that defines TTFT for each provider (first delta vs first byte).
- Whether OpenAI-compatible third-party servers reject `stream_options.include_usage`.
- Cached token semantics in each eino-ext provider.
- Whether the genai Vertex response converter keeps `sdkHttpResponse` for unary calls.
- Hand-rolled MCP server line count estimate (section 11).
- Whether major MCP clients (Claude Code, Cursor, and others) fall back to `initialize` against a
  legacy-only stdio server. The spec says a modern client with a legacy server "Fails", and the
  go-sdk client falls back (docs/protocol.md). Other clients were not verified.
