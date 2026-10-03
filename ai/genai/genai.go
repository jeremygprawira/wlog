// Package wlogenai turns a google.golang.org/genai response or stream into an llm.Record.
//
// FromGenerateContent maps a whole response. Observe wraps a stream, so the caller still
// reads every chunk and still builds the same Record: it re-yields each chunk through
// Next and Current, and the last non-nil usage metadata wins. Transport wraps the
// http.Client's RoundTripper, so it never replaces the authenticated transport NewClient
// builds around it.
//
// No helper keeps the prompt, the completion, or the tool payload unless the caller passes
// WithContent. Core redacts those values like any other.
package wlogenai

import (
	"context"
	"iter"
	"net/http"

	"google.golang.org/genai"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
)

// operation is the OTel gen_ai.operation.name value for this call shape.
const operation = "generate_content"

// Option configures a helper.
type Option func(*config)

// config holds the resolved options.
type config struct {
	content bool
}

// WithContent opts into the prompt, the completion, and the tool payload. Without it, the
// record holds the shape of the call and no text. Core redacts the values.
func WithContent() Option { return func(c *config) { c.content = true } }

// resolve applies the options.
func resolve(opts ...Option) config {
	c := config{}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// providerOf names the OTel gen_ai.provider.name value for backend.
func providerOf(backend genai.Backend) string {
	if backend == genai.BackendVertexAI {
		return "gcp.vertex_ai"
	}
	return "gcp.gemini"
}

// FromGenerateContent turns one GenerateContentResponse into an llm.Record. backend picks
// the provider name, because the same response shape serves the Gemini API and Vertex AI.
func FromGenerateContent(resp *genai.GenerateContentResponse, backend genai.Backend, opts ...Option) llm.Record {
	if resp == nil {
		return llm.Record{}
	}
	r := llm.Record{
		Provider:   providerOf(backend),
		Operation:  operation,
		Model:      resp.ModelVersion,
		ResponseID: resp.ResponseID,
	}
	applyUsage(&r, resp.UsageMetadata)
	applyCandidates(&r, resp.Candidates)
	if resolve(opts...).content {
		r.Content = contentOf(resp.Candidates)
	}
	return r
}

// applyUsage maps one usage block onto r, per the token table: the tool-use prompt count
// joins the input count, and the thoughts count joins the output count and doubles as the
// reasoning count. A nil usage leaves r unchanged.
func applyUsage(r *llm.Record, u *genai.GenerateContentResponseUsageMetadata) {
	if u == nil {
		return
	}
	r.InputTokens = int(u.PromptTokenCount + u.ToolUsePromptTokenCount)
	r.CachedInputTokens = int(u.CachedContentTokenCount)
	r.OutputTokens = int(u.CandidatesTokenCount + u.ThoughtsTokenCount)
	r.ReasoningTokens = int(u.ThoughtsTokenCount)
}

// applyCandidates reads the finish reason and the tool call names off every candidate.
func applyCandidates(r *llm.Record, candidates []*genai.Candidate) {
	for _, c := range candidates {
		if c.FinishReason != "" {
			r.FinishReason = string(c.FinishReason)
		}
		if c.Content == nil {
			continue
		}
		for _, p := range c.Content.Parts {
			if p.FunctionCall != nil {
				r.ToolCalls = append(r.ToolCalls, llm.ToolCall{Name: p.FunctionCall.Name})
			}
		}
	}
}

// contentOf maps the candidates' parts to the opt-in gen_ai shape.
func contentOf(candidates []*genai.Candidate) *llm.Content {
	content := &llm.Content{}
	for _, c := range candidates {
		if c.Content == nil {
			continue
		}
		for _, p := range c.Content.Parts {
			switch {
			case p.Text != "":
				content.OutputMessages = append(content.OutputMessages, llm.Message{
					Role:  "model",
					Parts: []llm.Part{{Type: "text", Content: p.Text}},
				})
			case p.FunctionCall != nil:
				content.OutputMessages = append(content.OutputMessages, llm.Message{
					Role:  "model",
					Parts: []llm.Part{{Type: "tool_call", ID: p.FunctionCall.ID, Name: p.FunctionCall.Name, Arguments: p.FunctionCall.Args}},
				})
			}
		}
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// Observer wraps a GenerateContentStream and builds a Record as the caller reads it. The
// caller drives it with Next and Current, the same shape as the Anthropic and OpenAI
// observers, over a stdlib iter.Seq2 pulled one step at a time.
type Observer struct {
	next    func() (*genai.GenerateContentResponse, error, bool)
	stop    func()
	current *genai.GenerateContentResponse
	err     error
	record  llm.Record
	opts    []Option
}

// Observe wraps seq. The caller drives the Observer with Next and Current, and reads
// Record once Next reports false.
func Observe(seq iter.Seq2[*genai.GenerateContentResponse, error], backend genai.Backend, opts ...Option) *Observer {
	next, stop := iter.Pull2(seq)
	return &Observer{
		next:   next,
		stop:   stop,
		record: llm.Record{Provider: providerOf(backend), Operation: operation},
		opts:   opts,
	}
}

// Next reads the next chunk and folds it into the record. It reports false at the end or
// on a stream error, which Err then reports.
func (o *Observer) Next() bool {
	resp, err, ok := o.next()
	if !ok {
		return false
	}
	if err != nil {
		o.err = err
		o.stop()
		return false
	}
	o.current = resp
	o.consume(resp)
	return true
}

// Current returns the chunk the caller just read.
func (o *Observer) Current() *genai.GenerateContentResponse { return o.current }

// Record returns the record built so far. Read it after Next reports false.
func (o *Observer) Record() llm.Record { return o.record }

// Err returns the stream error, if any.
func (o *Observer) Err() error { return o.err }

// consume folds one chunk into the record. Usage metadata overwrites what came before,
// because the last non-nil block a stream sends is the authoritative total.
func (o *Observer) consume(resp *genai.GenerateContentResponse) {
	if resp == nil {
		return
	}
	if resp.ResponseID != "" {
		o.record.ResponseID = resp.ResponseID
	}
	if resp.ModelVersion != "" {
		o.record.Model = resp.ModelVersion
	}
	applyUsage(&o.record, resp.UsageMetadata)
	applyCandidates(&o.record, resp.Candidates)
	if resolve(o.opts...).content {
		if c := contentOf(resp.Candidates); c != nil {
			if o.record.Content == nil {
				o.record.Content = &llm.Content{}
			}
			o.record.Content.OutputMessages = append(o.record.Content.OutputMessages, c.OutputMessages...)
		}
	}
}

// Transport wraps next, so every round trip records its attempt count onto the llm group
// of the current event. A nil next means http.DefaultTransport. Set it as the Transport of
// the http.Client passed in ClientConfig.HTTPClient, before NewClient wraps that client
// with its own authorization middleware, so the authenticated transport is wrapped and
// never replaced.
func Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &transport{next: next}
}

// transport counts attempts around the wrapped round tripper.
type transport struct{ next http.RoundTripper }

// RoundTrip records one attempt and returns exactly what the wrapped round tripper
// returned.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	recordAttempt(req.Context())
	return resp, err
}

// recordAttempt adds one to the attempts count on the event of ctx.
func recordAttempt(ctx context.Context) {
	wlog.UpdateGroup(ctx, "llm", func(existing map[string]any) {
		attempts := 1
		switch n := existing["attempts"].(type) {
		case int:
			attempts = n + 1
		case int64:
			attempts = int(n) + 1
		}
		existing["attempts"] = attempts
	})
}
