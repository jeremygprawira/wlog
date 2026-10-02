// Package honeycomb sends wlog events to Honeycomb's batch API. It reads
// HONEYCOMB_API_KEY, HONEYCOMB_DATASET, and HONEYCOMB_API_URL when an option does not set
// them.
//
// Honeycomb answers one status per event, so a batch can succeed in part. The drain
// returns a pipeline.PartialError, and the pipeline retries only the events Honeycomb
// asked for again.
package honeycomb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// defaultAPIURL is Honeycomb's US endpoint. An EU team sets the EU one.
const defaultAPIURL = "https://api.honeycomb.io"

// Limits Honeycomb documents. A field over the count cap is dropped, a string is cut, and
// an event over the byte cap is dropped whole.
const (
	maxFields       = 2000
	maxStringBytes  = 64 * 1024
	maxEventBytes   = 1 << 20
	maxRequestBytes = 5 << 20
)

// config holds the resolved configuration.
type config struct {
	key          string
	dataset      string
	apiURL       string
	spans        bool
	httpClient   *http.Client
	timeout      time.Duration
	timeoutSet   bool
	userAgent    string
	userAgentSet bool
	gzip         bool
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithAPIKey sets the Honeycomb team key. Overrides HONEYCOMB_API_KEY.
func WithAPIKey(key string) Option { return func(c *config) { c.key = key } }

// WithDataset sends every event to one dataset. Overrides HONEYCOMB_DATASET. Without a
// dataset, each event goes to the dataset named by its service.name.
func WithDataset(dataset string) Option { return func(c *config) { c.dataset = dataset } }

// WithAPIURL sets the API endpoint. Overrides HONEYCOMB_API_URL. EU teams pass
// https://api.eu1.honeycomb.io.
func WithAPIURL(apiURL string) Option { return func(c *config) { c.apiURL = apiURL } }

// WithSpans adds name and error to each event, so Honeycomb shows it as a span. The
// default is true. Turn it off when OTel spans reach the same environment.
func WithSpans(on bool) Option { return func(c *config) { c.spans = on } }

// WithHTTPClient uses a caller-supplied HTTP client. wlog never changes it.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithTimeout sets the HTTP request timeout. Default 10s.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
		c.timeoutSet = true
	}
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *config) {
		c.userAgent = ua
		c.userAgentSet = true
	}
}

// WithGzip compresses the body when on.
func WithGzip(on bool) Option { return func(c *config) { c.gzip = on } }

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender posts batches to Honeycomb. It implements pipeline.Sender.
type Sender struct {
	apiURL  string
	dataset string
	spans   bool
	base    []httpdrain.Option

	mu      sync.Mutex
	clients map[string]*httpdrain.Client
}

// New returns the drain with the pipeline defaults, or with the options WithPipeline set.
func New(opts ...Option) (wlog.Drain, error) {
	s, popts, err := newSender(opts...)
	if err != nil {
		return nil, err
	}
	return pipeline.Wrap(s, popts...), nil
}

// NewSender returns the raw sender, for a caller that builds its own pipeline.
func NewSender(opts ...Option) (*Sender, error) {
	s, _, err := newSender(opts...)
	return s, err
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// newSender resolves one configuration from opts and the environment.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		key:     os.Getenv("HONEYCOMB_API_KEY"),
		dataset: os.Getenv("HONEYCOMB_DATASET"),
		apiURL:  firstEnv("HONEYCOMB_API_URL", "HONEYCOMB_API_ENDPOINT"),
		spans:   true,
		gzip:    true,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.key == "" {
		return nil, nil, fmt.Errorf("honeycomb: HONEYCOMB_API_KEY is required")
	}
	apiURL, err := normalizeURL(c.apiURL)
	if err != nil {
		return nil, nil, err
	}
	base := []httpdrain.Option{
		httpdrain.WithSource("honeycomb"),
		httpdrain.WithHeader("X-Honeycomb-Team", c.key),
	}
	if c.httpClient != nil {
		base = append(base, httpdrain.WithHTTPClient(c.httpClient))
	}
	if c.timeoutSet {
		base = append(base, httpdrain.WithTimeout(c.timeout))
	}
	if c.userAgentSet {
		base = append(base, httpdrain.WithUserAgent(c.userAgent))
	}
	if c.gzip {
		base = append(base, httpdrain.WithGzip(true))
	}
	return &Sender{
		apiURL:  apiURL,
		dataset: c.dataset,
		spans:   c.spans,
		base:    base,
		clients: map[string]*httpdrain.Client{},
	}, c.pipelineOpts, nil
}

// SendBatch posts the events, split per dataset and per request byte limit, and maps the
// per-item statuses to a PartialError. A batch that Honeycomb accepts in full returns nil.
//
// An event over the per-event byte limit is dropped whole, because no request can carry it.
// A chunk that Honeycomb refuses with a 413 is halved, so one large request does not lose
// the batch.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	groups, order := s.group(events)
	var retry, dropped []int
	reason := ""
	for _, dataset := range order {
		indexes := groups[dataset]
		// The group's events, in batch order, so a chunk names its batch indexes.
		group := make([]map[string]any, 0, len(indexes))
		for _, i := range indexes {
			group = append(group, events[i])
		}
		// An event over the per-event limit is dropped whole, because no request can
		// carry it. The rest are split at the per-request limit.
		kept := make([]map[string]any, 0, len(group))
		keptIndexes := make([]int, 0, len(group))
		for i, event := range group {
			if s.itemBytes(event) > maxEventBytes {
				dropped = append(dropped, indexes[i])
				reason = "too_large"
				continue
			}
			kept = append(kept, event)
			keptIndexes = append(keptIndexes, indexes[i])
		}
		chunks, _ := httpdrain.Chunks(kept, 0, maxRequestBytes, s.itemBytes)
		client := s.clientFor(dataset)
		for _, chunk := range chunks {
			post := func(ctx context.Context, chunkEvents []map[string]any) error {
				return s.postChunk(ctx, client, chunkEvents)
			}
			pe, err := httpdrain.SendChunk(ctx, kept, chunk, post)
			switch {
			case err != nil:
				var chunkPE *pipeline.PartialError
				if errors.As(err, &chunkPE) {
					// Per-item results for this chunk, in group positions.
					retry = append(retry, mapIndexes(keptIndexes, chunkPE.Retry)...)
					dropped = append(dropped, mapIndexes(keptIndexes, chunkPE.Dropped)...)
					if chunkPE.Reason != "" {
						reason = chunkPE.Reason
					}
					continue
				}
				// The request failed as a whole: this chunk is retried or dropped, and
				// the datasets and chunks that landed are left alone.
				again := retryable(err)
				for i := chunk.Start; i < chunk.End; i++ {
					if again {
						retry = append(retry, keptIndexes[i])
					} else {
						dropped = append(dropped, keptIndexes[i])
					}
				}
				if reason == "" {
					reason = statusReason(err)
				}
			case pe != nil:
				dropped = append(dropped, mapIndexes(keptIndexes, pe.Dropped)...)
				reason = pe.Reason
			}
		}
	}
	if len(retry) == 0 && len(dropped) == 0 {
		return nil
	}
	return &pipeline.PartialError{Retry: retry, Dropped: dropped, Reason: reason}
}

// itemBytes returns the encoded size of one event's item, which is what the request holds.
func (s *Sender) itemBytes(event map[string]any) int {
	body, err := json.Marshal([]map[string]any{s.itemOf(event)})
	if err != nil {
		return 0
	}
	return len(body)
}

// postChunk posts one chunk and maps the per-item statuses to a PartialError, whose
// indexes are positions in the events the caller passed.
func (s *Sender) postChunk(ctx context.Context, client *httpdrain.Client, events []map[string]any) error {
	items := make([]map[string]any, 0, len(events))
	for _, event := range events {
		items = append(items, s.itemOf(event))
	}
	body, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("honeycomb: marshal batch: %w", err)
	}
	answer, err := client.PostFor(ctx, body, "application/json")
	if err != nil {
		return err
	}
	var results []struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(answer, &results); err != nil {
		return fmt.Errorf("honeycomb: read response: %w", err)
	}
	var retry, dropped []int
	reason := ""
	for j, result := range results {
		if j >= len(events) {
			break
		}
		switch {
		case result.Status == http.StatusAccepted:
		case result.Status >= 500:
			retry = append(retry, j)
			reason = "status_" + strconv.Itoa(result.Status)
		default:
			dropped = append(dropped, j)
			reason = "status_" + strconv.Itoa(result.Status)
		}
	}
	if len(retry) == 0 && len(dropped) == 0 {
		return nil
	}
	return &pipeline.PartialError{Retry: retry, Dropped: dropped, Reason: reason}
}

// mapIndexes turns group positions into batch positions.
func mapIndexes(batch []int, local []int) []int {
	out := make([]int, 0, len(local))
	for _, i := range local {
		if i >= 0 && i < len(batch) {
			out = append(out, batch[i])
		}
	}
	return out
}

// retryable reports whether a failed request is worth another try. A status the backend
// marks retryable, and a transport error, are retryable. Anything else is final.
func retryable(err error) bool {
	var statusErr *httpdrain.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Retryable()
	}
	return true
}

// statusReason names a failed request for the PartialError. It never holds an event value.
func statusReason(err error) string {
	var statusErr *httpdrain.StatusError
	if errors.As(err, &statusErr) {
		return "status_" + strconv.Itoa(statusErr.Status)
	}
	return "transport"
}

// group collects the batch indexes of each dataset, in first-seen order.
func (s *Sender) group(events []map[string]any) (map[string][]int, []string) {
	groups := map[string][]int{}
	order := []string{}
	for i, event := range events {
		dataset := s.datasetOf(event)
		if _, ok := groups[dataset]; !ok {
			order = append(order, dataset)
		}
		groups[dataset] = append(groups[dataset], i)
	}
	return groups, order
}

// datasetOf returns the dataset of one event: the fixed one, else service.name, else a
// placeholder, so an event without a service name still reaches Honeycomb.
func (s *Sender) datasetOf(event map[string]any) string {
	if s.dataset != "" {
		return s.dataset
	}
	if name, ok := pathValue(event, "service.name"); ok {
		if text, ok := name.(string); ok && text != "" {
			return text
		}
	}
	return "unknown_service"
}

// clientFor returns the client for one dataset, building it once.
func (s *Sender) clientFor(dataset string) *httpdrain.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client, ok := s.clients[dataset]; ok {
		return client
	}
	client := httpdrain.New(s.apiURL+"/1/batch/"+url.PathEscape(dataset), s.base...)
	s.clients[dataset] = client
	return client
}

// itemOf builds one Honeycomb batch item.
func (s *Sender) itemOf(event map[string]any) map[string]any {
	return map[string]any{
		"time":       timestampOf(event),
		"samplerate": samplerateOf(event),
		"data":       s.dataOf(event),
	}
}

// dataOf returns the event as dotted keys at every depth. An array becomes one JSON
// string, because Honeycomb holds no array.
func (s *Sender) dataOf(event map[string]any) map[string]any {
	flat := map[string]any{}
	flatten(flat, "", event)
	data := make(map[string]any, len(flat)+3)
	for key, value := range flat {
		data[key] = dataValue(value)
	}
	if s.spans {
		if operation, _ := event["operation"].(string); operation != "" {
			data["name"] = operation
		}
		if outcome, _ := event["outcome"].(string); outcome == "error" {
			data["error"] = true
		}
		if parent, ok := pathValue(event, "trace.parent_span_id"); ok {
			if text, _ := parent.(string); text != "" {
				data["trace.parent_id"] = text
			}
		}
	} else {
		delete(data, "trace.span_id")
		delete(data, "trace.parent_id")
	}
	return capFields(data, maxFields)
}

// flatten copies a nested map into dotted keys.
func flatten(out map[string]any, prefix string, value any) {
	nested, ok := value.(map[string]any)
	if !ok {
		if prefix != "" {
			out[prefix] = value
		}
		return
	}
	for key, child := range nested {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		flatten(out, path, child)
	}
}

// dataValue converts one value for the Honeycomb data object.
func dataValue(value any) any {
	if text, ok := value.(string); ok {
		return cutString(text, maxStringBytes)
	}
	if _, ok := value.([]any); ok {
		body, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(body)
	}
	return value
}

// cutString cuts a long string and marks it with an ellipsis.
func cutString(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max-len("…")] + "…"
}

// capFields keeps at most max fields. The keys are sorted, so the same event always keeps
// the same fields.
func capFields(data map[string]any, max int) map[string]any {
	if len(data) <= max {
		return data
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys[max:] {
		delete(data, key)
	}
	return data
}

// timestampOf reads the event timestamp, or now.
func timestampOf(event map[string]any) string {
	if timestamp, _ := event["timestamp"].(string); timestamp != "" {
		return timestamp
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// samplerateOf returns 100 divided by the sample rate, rounded and at least 1, so a
// sampled event keeps its weight in Honeycomb.
func samplerateOf(event map[string]any) int {
	rate, ok := numberAt(event, "wlog.sample_rate")
	if !ok || rate <= 0 || rate >= 100 {
		return 1
	}
	value := int(math.Round(100 / rate))
	if value < 1 {
		return 1
	}
	return value
}

// pathValue reads a dotted path from an event.
func pathValue(event map[string]any, path string) (any, bool) {
	var current any = event
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// numberAt reads a number at a dotted path.
func numberAt(event map[string]any, path string) (float64, bool) {
	value, ok := pathValue(event, path)
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

// firstEnv returns the first of the names that holds a value.
func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

// normalizeURL turns the configured API URL into a base URL.
func normalizeURL(apiURL string) (string, error) {
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		return defaultAPIURL, nil
	}
	if !strings.Contains(apiURL, "://") {
		apiURL = "https://" + apiURL
	}
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return "", fmt.Errorf("honeycomb: api url %q: %w", apiURL, err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("honeycomb: api url %q has no host", apiURL)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
