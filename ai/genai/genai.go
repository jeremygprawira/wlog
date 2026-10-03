// Package wlogenai turns a google.golang.org/genai response or stream into an llm.Record.
//
// FromGenerateContent maps a whole response. Observe wraps a stream and re-yields each
// chunk. The caller stops the stream by stopping the range. The last non-nil usage
// metadata wins. Transport wraps the
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

// Observe wraps seq and re-yields each chunk. Stopping the range stops the stream, so
// an early return does not leave the pull or the HTTP body running. The returned
// function reads the record built so far.
func Observe(seq iter.Seq2[*genai.GenerateContentResponse, error], backend genai.Backend, opts ...Option) (iter.Seq2[*genai.GenerateContentResponse, error], func() llm.Record) {
	next, stop := iter.Pull2(seq)
	record := llm.Record{Provider: providerOf(backend), Operation: operation}
	out := func(yield func(*genai.GenerateContentResponse, error) bool) {
		defer stop()
		for {
			resp, err, ok := next()
			if !ok {
				return
			}
			if err != nil {
				yield(resp, err)
				return
			}
			consume(&record, resp, opts)
			if !yield(resp, nil) {
				return
			}
		}
	}
	return out, func() llm.Record { return record }
}

// consume folds one chunk into the record. Usage metadata overwrites what came before,
// because the last non-nil block a stream sends is the authoritative total.
func consume(record *llm.Record, resp *genai.GenerateContentResponse, opts []Option) {
	if resp == nil {
		return
	}
	if resp.ResponseID != "" {
		record.ResponseID = resp.ResponseID
	}
	if resp.ModelVersion != "" {
		record.Model = resp.ModelVersion
	}
	applyUsage(record, resp.UsageMetadata)
	applyCandidates(record, resp.Candidates)
	if resolve(opts...).content {
		if c := contentOf(resp.Candidates); c != nil {
			if record.Content == nil {
				record.Content = &llm.Content{}
			}
			record.Content.OutputMessages = append(record.Content.OutputMessages, c.OutputMessages...)
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
