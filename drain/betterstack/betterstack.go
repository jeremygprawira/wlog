// Package betterstack sends wlog events to Better Stack's logs intake. It reads
// BETTERSTACK_SOURCE_TOKEN and BETTERSTACK_HOST when an option does not set them.
package betterstack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// defaultHost is Better Stack's intake host.
const defaultHost = "https://in.logs.betterstack.com"

// config holds the resolved configuration.
type config struct {
	token        string
	host         string
	httpClient   *http.Client
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithSourceToken sets the source token. Overrides BETTERSTACK_SOURCE_TOKEN.
func WithSourceToken(token string) Option { return func(c *config) { c.token = token } }

// WithHTTPClient uses a caller-supplied HTTP client. wlog never changes it.
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

// WithHost sets the intake host. Overrides BETTERSTACK_INGESTING_HOST and its older alias
// BETTERSTACK_HOST. A bare host takes https.
func WithHost(host string) Option { return func(c *config) { c.host = host } }

// Sender posts batches to Better Stack. It implements pipeline.Sender.
type Sender struct {
	client *httpdrain.Client
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

// newSender resolves one configuration from opts and the environment.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		token: os.Getenv("BETTERSTACK_SOURCE_TOKEN"),
		// The ingesting host is the current name; the older name stays an alias.
		host: firstEnv("BETTERSTACK_INGESTING_HOST", "BETTERSTACK_HOST"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.token == "" {
		return nil, nil, fmt.Errorf("betterstack: BETTERSTACK_SOURCE_TOKEN is required")
	}
	host, err := normalizeHost(c.host)
	if err != nil {
		return nil, nil, err
	}
	clientOpts := []httpdrain.Option{
		httpdrain.WithSource("betterstack"),
		httpdrain.WithHeader("Authorization", "Bearer "+c.token),
	}
	if c.httpClient != nil {
		clientOpts = append(clientOpts, httpdrain.WithHTTPClient(c.httpClient))
	}
	return &Sender{client: httpdrain.New(host, clientOpts...)}, c.pipelineOpts, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
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

// normalizeHost turns the configured host into an intake URL. A bare host gets https, and
// a value with no host at all is refused, so a typo fails at startup rather than at the
// first batch.
func normalizeHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return defaultHost, nil
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	parsed, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("betterstack: host %q: %w", host, err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("betterstack: host %q has no host", host)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// SendBatch posts one JSON array. The event goes over as it is, nested, with dt, level,
// and message mapped so Better Stack's own columns line up.
func (d *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	items := make([]map[string]any, 0, len(events))
	for _, event := range events {
		item := make(map[string]any, len(event)+3)
		for key, value := range event {
			item[key] = value
		}
		if _, ok := item["dt"]; !ok {
			item["dt"] = timestampOf(event)
		}
		if level, _ := item["level"].(string); level == "" {
			item["level"] = "info"
		}
		if message, _ := item["message"].(string); message == "" {
			message, _ = item["operation"].(string)
			item["message"] = message
		}
		items = append(items, item)
	}
	body, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("betterstack: marshal batch: %w", err)
	}
	return d.client.Post(ctx, body, "application/json")
}

// timestampOf reads the event timestamp, or now.
func timestampOf(event map[string]any) string {
	if timestamp, _ := event["timestamp"].(string); timestamp != "" {
		return timestamp
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}
