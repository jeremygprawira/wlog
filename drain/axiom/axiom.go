// Package axiom sends wlog events to Axiom's ingest API as NDJSON. It reads
// AXIOM_TOKEN, AXIOM_DATASET, and AXIOM_URL when an option does not set them.
package axiom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// defaultURL is Axiom's public API host.
const defaultURL = "https://api.axiom.co"

// config holds the resolved configuration for one Sender.
type config struct {
	token        string
	dataset      string
	url          string
	gzip         bool
	httpClient   *http.Client
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithToken sets the Axiom API token, used as a bearer token. Overrides AXIOM_TOKEN.
func WithToken(token string) Option { return func(c *config) { c.token = token } }

// WithDataset sets the Axiom dataset that receives the events. Overrides AXIOM_DATASET.
func WithDataset(dataset string) Option { return func(c *config) { c.dataset = dataset } }

// WithURL sets the Axiom API base URL. Overrides AXIOM_URL. The default is
// https://api.axiom.co.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithGzip compresses the request body. Off by default.
func WithGzip(on bool) Option { return func(c *config) { c.gzip = on } }

// WithHTTPClient uses a caller-supplied HTTP client, for a custom transport or
// instrumentation. wlog never changes the client it is given.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender sends batches to one Axiom dataset. It implements pipeline.Sender.
type Sender struct {
	client *httpdrain.Client
}

// New returns the Axiom drain with the pipeline defaults, or with the options WithPipeline
// set.
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

// newSender resolves one configuration from opts and the AXIOM_* env vars. It returns an
// error when the token or the dataset is missing.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		token:   os.Getenv("AXIOM_TOKEN"),
		dataset: os.Getenv("AXIOM_DATASET"),
		url:     os.Getenv("AXIOM_URL"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.token == "" {
		return nil, nil, fmt.Errorf("axiom: AXIOM_TOKEN is required")
	}
	if c.dataset == "" {
		return nil, nil, fmt.Errorf("axiom: AXIOM_DATASET is required")
	}
	if c.url == "" {
		c.url = defaultURL
	}
	c.url = strings.TrimRight(c.url, "/")

	clientOpts := []httpdrain.Option{
		httpdrain.WithGzip(c.gzip),
		httpdrain.WithSource("axiom"),
		httpdrain.WithHeader("Authorization", "Bearer "+c.token),
	}
	if c.httpClient != nil {
		clientOpts = append(clientOpts, httpdrain.WithHTTPClient(c.httpClient))
	}
	client := httpdrain.New(c.url+"/v1/datasets/"+c.dataset+"/ingest", clientOpts...)
	return &Sender{client: client}, c.pipelineOpts, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// SendBatch posts one NDJSON request. One event is one line, so Axiom ingests each
// event as its own record. The error is returned unchanged, so pipeline can read the
// retryable status from an httpdrain.StatusError.
func (d *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var buf strings.Builder
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("axiom: marshal event: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return d.client.Post(ctx, []byte(buf.String()), "application/x-ndjson")
}
