// Package elastic sends wlog events to Elasticsearch and OpenSearch with the bulk API.
// It reads ELASTICSEARCH_URL or OPENSEARCH_URL, and the credential and index variables,
// when an option does not set them.
//
// Each event becomes one bulk `create` line and one ECS line, because a data stream
// accepts only `create`. Elasticsearch answers one item per event, so the drain returns a
// pipeline.PartialError and the pipeline retries only the events the backend asked for
// again.
//
// Amazon OpenSearch Service needs SigV4. Pass a signing client through WithHTTPClient.
// The aws-sdk-go-v2 v4 signer builds one, so the root module gains no dependency:
//
//	cfg, _ := config.LoadDefaultConfig(ctx)
//	client := &http.Client{Transport: awshttp.NewBuildableClient().WithTransportOptions(
//		func(t *http.Transport) { t.RoundTripper = v4.NewSigner().SignHTTP }...)}
//	// or wrap the transport with the signer, and pass the client to WithHTTPClient.
//
// The drain never installs the index template. Run the PUT by hand:
//
//	PUT _index_template/logs-wlog
//	<the bytes of Template(Elasticsearch)>
package elastic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
	"github.com/jeremygprawira/wlog/preset"
)

// Engine selects the index template flavor.
type Engine int

const (
	// Elasticsearch is the default engine.
	Elasticsearch Engine = iota
	// OpenSearch uses flat_object for the user key map.
	OpenSearch
)

// Defaults the spec names.
const (
	defaultIndex         = "logs-wlog-default"
	defaultMaxBatchBytes = 5 << 20
	maxBatchBytesCap     = 100 << 20
	bulkQuery            = "filter_path=errors,items.*.status,items.*.error.type"
)

// config holds the resolved configuration.
type config struct {
	url          string
	apiKey       string
	user         string
	password     string
	index        string
	maxBatch     int
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

// WithURL sets the cluster URL. Overrides ELASTICSEARCH_URL and OPENSEARCH_URL.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithAPIKey sets an encoded API key, sent as Authorization: ApiKey <encoded>.
func WithAPIKey(encoded string) Option { return func(c *config) { c.apiKey = encoded } }

// WithBasicAuth sets a user and password, sent as basic auth.
func WithBasicAuth(user, password string) Option {
	return func(c *config) {
		c.user = user
		c.password = password
	}
}

// WithIndex sets the index or data stream. Default logs-wlog-default.
func WithIndex(name string) Option { return func(c *config) { c.index = name } }

// WithMaxBatchBytes sets the body cap, clamped to 100 MiB. Default 5 MiB.
func WithMaxBatchBytes(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxBatch = min(n, maxBatchBytesCap)
		}
	}
}

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

// Sender posts bulk bodies to one cluster. It implements pipeline.Sender.
type Sender struct {
	client   *httpdrain.Client
	maxBatch int
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
		url:      firstEnv("ELASTICSEARCH_URL", "OPENSEARCH_URL"),
		apiKey:   os.Getenv("ELASTICSEARCH_API_KEY"),
		user:     os.Getenv("ELASTICSEARCH_USERNAME"),
		password: os.Getenv("ELASTICSEARCH_PASSWORD"),
		index:    os.Getenv("ELASTICSEARCH_INDEX"),
		maxBatch: defaultMaxBatchBytes,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.url == "" {
		return nil, nil, fmt.Errorf("elastic: ELASTICSEARCH_URL is required")
	}
	if c.index == "" {
		c.index = defaultIndex
	}
	clientOpts := []httpdrain.Option{httpdrain.WithSource("elastic")}
	switch {
	case c.apiKey != "":
		clientOpts = append(clientOpts, httpdrain.WithHeader("Authorization", "ApiKey "+c.apiKey))
	case c.user != "" || c.password != "":
		clientOpts = append(clientOpts, httpdrain.WithBasicAuth(c.user, c.password))
	}
	if c.httpClient != nil {
		clientOpts = append(clientOpts, httpdrain.WithHTTPClient(c.httpClient))
	}
	if c.timeoutSet {
		clientOpts = append(clientOpts, httpdrain.WithTimeout(c.timeout))
	}
	if c.userAgentSet {
		clientOpts = append(clientOpts, httpdrain.WithUserAgent(c.userAgent))
	}
	if c.gzip {
		clientOpts = append(clientOpts, httpdrain.WithGzip(true))
	}
	return &Sender{
		client:   httpdrain.New(bulkURL(c.url, c.index), clientOpts...),
		maxBatch: c.maxBatch,
	}, c.pipelineOpts, nil
}

// SendBatch posts the events in one bulk request, split at the byte cap, and maps the
// item results to a PartialError.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var retry, dropped []int
	reason := ""
	for start := 0; start < len(events); {
		end := s.chunkEnd(events, start)
		body, err := bulkBody(events[start:end])
		if err != nil {
			return fmt.Errorf("elastic: build bulk body: %w", err)
		}
		answer, err := s.client.PostFor(ctx, body, "application/x-ndjson")
		if err != nil {
			return err
		}
		chunkRetry, chunkDropped, chunkReason, err := itemResults(answer, start, end-start)
		if err != nil {
			return err
		}
		retry = append(retry, chunkRetry...)
		dropped = append(dropped, chunkDropped...)
		if chunkReason != "" {
			reason = chunkReason
		}
		start = end
	}
	if len(retry) == 0 && len(dropped) == 0 {
		return nil
	}
	return &pipeline.PartialError{Retry: retry, Dropped: dropped, Reason: reason}
}

// chunkEnd returns the end index of the next chunk, so the body stays under the byte cap.
// One event is always in a chunk, even when it alone passes the cap.
func (s *Sender) chunkEnd(events []map[string]any, start int) int {
	size := 0
	for i := start; i < len(events); i++ {
		line, err := eventLines(events[i])
		if err != nil {
			return i + 1
		}
		if i > start && size+len(line) > s.maxBatch {
			return i
		}
		size += len(line)
	}
	return len(events)
}

// bulkBody builds the ndjson body: a create line and an ECS line per event.
func bulkBody(events []map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	for _, event := range events {
		line, err := eventLines(event)
		if err != nil {
			return nil, err
		}
		buf.WriteString("{\"create\":{}}\n")
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// eventLines returns the ECS line for one event.
func eventLines(event map[string]any) ([]byte, error) {
	return json.Marshal(preset.ECS().Apply(event))
}

// bulkResponse is the filtered bulk answer: the error flag and one item per event.
type bulkResponse struct {
	Errors bool                  `json:"errors"`
	Items  []map[string]bulkItem `json:"items"`
}

// bulkItem is the result of one bulk line.
type bulkItem struct {
	Status int `json:"status"`
	Error  struct {
		Type string `json:"type"`
	} `json:"error"`
}

// itemResults maps the item statuses to batch indexes. It never reads error.reason,
// because a reason can quote a field value.
func itemResults(answer []byte, offset, count int) (retry, dropped []int, reason string, err error) {
	var response bulkResponse
	if err := json.Unmarshal(answer, &response); err != nil {
		return nil, nil, "", fmt.Errorf("elastic: read response: %w", err)
	}
	for i, item := range response.Items {
		if i >= count {
			break
		}
		result := item["create"]
		switch {
		case result.Status >= 200 && result.Status < 300:
		case result.Status == http.StatusTooManyRequests || result.Status >= 500:
			retry = append(retry, offset+i)
			reason = result.Error.Type
		default:
			dropped = append(dropped, offset+i)
			reason = result.Error.Type
		}
	}
	return retry, dropped, reason, nil
}

// bulkURL builds the bulk endpoint for one index.
func bulkURL(base, index string) string {
	return strings.TrimRight(base, "/") + "/" + index + "/_bulk?" + bulkQuery
}

// indexTemplate is the composable template one Engine needs.
type indexTemplate struct {
	IndexPatterns []string `json:"index_patterns"`
	Priority      int      `json:"priority"`
	DataStream    struct{} `json:"data_stream"`
	Template      struct {
		Mappings struct {
			Properties map[string]map[string]string `json:"properties"`
		} `json:"mappings"`
	} `json:"template"`
}

// Template returns the composable index template for logs-wlog-*, with data_stream on and
// priority 200. The drain never installs it.
func Template(engine Engine) []byte {
	fields := map[string]string{
		"@timestamp":                "date",
		"event.duration":            "long",
		"http.response.status_code": "long",
		"client.ip":                 "ip",
		"event.id":                  "keyword",
		"trace.id":                  "keyword",
		"span.id":                   "keyword",
		"log.level":                 "keyword",
		"service.name":              "keyword",
		"service.version":           "keyword",
		"service.environment":       "keyword",
		"service.node.name":         "keyword",
		"wlog.fields":               "flattened",
	}
	if engine == OpenSearch {
		fields["wlog.fields"] = "flat_object"
	}
	properties := make(map[string]map[string]string, len(fields))
	for name, kind := range fields {
		properties[name] = map[string]string{"type": kind}
	}
	t := indexTemplate{IndexPatterns: []string{"logs-wlog-*"}, Priority: 200}
	t.Template.Mappings.Properties = properties
	body, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil
	}
	return append(body, '\n')
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
