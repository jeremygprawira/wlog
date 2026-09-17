// Package datadog sends wlog events to the Datadog logs intake v2 as a JSON array. It
// reads DD_API_KEY and DD_SITE when an option does not set them.
package datadog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// The Datadog logs intake refuses a request over 1000 items or 5 MB, so a batch is split
// to fit before it is sent.
const (
	maxBatchItems = 1000
	maxBatchBytes = 5 << 20
)

// Defaults for the Datadog US site and the wlog source tag.
const (
	defaultSite   = "datadoghq.com"
	defaultSource = "wlog"
)

// config holds the resolved configuration for one Sender.
type config struct {
	apiKey       string
	site         string
	url          string
	source       string
	httpClient   *http.Client
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithAPIKey sets the Datadog API key. Overrides DD_API_KEY.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithSite sets the Datadog site, for example datadoghq.eu. Overrides DD_SITE.
func WithSite(site string) Option { return func(c *config) { c.site = site } }

// WithURL sets the full intake URL. It replaces the URL built from the site, for a
// proxy or a test.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithSource sets the ddsource value. Default wlog.
func WithSource(source string) Option { return func(c *config) { c.source = source } }

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

// Sender posts batches to the Datadog logs intake. It implements pipeline.Sender.
type Sender struct {
	client  *httpdrain.Client
	source  string
	service string
	env     string
}

// New returns the Datadog drain with the pipeline defaults, or with the options
// WithPipeline set.
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

// newSender resolves one configuration. It returns an error when DD_API_KEY is missing.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		apiKey: os.Getenv("DD_API_KEY"),
		site:   os.Getenv("DD_SITE"),
		source: defaultSource,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.apiKey == "" {
		return nil, nil, errors.New("datadog: DD_API_KEY is required")
	}
	if c.site == "" {
		c.site = defaultSite
	}
	url := c.url
	if url == "" {
		url = intakeURL(c.site)
	}
	clientOpts := []httpdrain.Option{
		httpdrain.WithSource("datadog"),
		httpdrain.WithHeader("DD-API-KEY", c.apiKey),
	}
	if c.httpClient != nil {
		clientOpts = append(clientOpts, httpdrain.WithHTTPClient(c.httpClient))
	}
	// DD_SERVICE and DD_ENV are read once here, so a batch never depends on when the
	// environment was read.
	return &Sender{
		client:  httpdrain.New(url, clientOpts...),
		source:  c.source,
		service: os.Getenv("DD_SERVICE"),
		env:     os.Getenv("DD_ENV"),
	}, c.pipelineOpts, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// intakeURL builds the intake URL for a site, for example
// https://http-intake.logs.datadoghq.eu/api/v2/logs.
func intakeURL(site string) string {
	site = strings.TrimPrefix(site, "https://")
	site = strings.TrimSuffix(site, "/")
	return "https://http-intake.logs." + site + "/api/v2/logs"
}

// logItem is one element of the intake JSON array.
type logItem struct {
	DDSource string `json:"ddsource"`
	Service  string `json:"service,omitempty"`
	DDTags   string `json:"ddtags,omitempty"`
	Level    string `json:"level,omitempty"`
	Message  string `json:"message"`
}

// SendBatch splits the events to fit the intake's limits and posts each chunk, so a big
// batch is several acceptable requests rather than one refused one. A chunk that still
// gets a 413 splits in half, and only that chunk's halves are sent again.
func (d *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	for _, chunk := range d.splitBatches(events) {
		if err := d.sendChunk(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

// splitBatches cuts events into runs that hold at most maxBatchItems items and at most
// maxBatchBytes of encoded body.
func (d *Sender) splitBatches(events []map[string]any) [][]map[string]any {
	var out [][]map[string]any
	var current []map[string]any
	size := 0
	for _, event := range events {
		itemSize := d.itemSize(event)
		if len(current) > 0 && (len(current) >= maxBatchItems || size+itemSize > maxBatchBytes) {
			out = append(out, current)
			current, size = nil, 0
		}
		current = append(current, event)
		size += itemSize
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

// itemSize is one event's encoded size, used to keep a chunk under the intake limit.
func (d *Sender) itemSize(event map[string]any) int {
	body, err := json.Marshal(d.items([]map[string]any{event}))
	if err != nil {
		return 0
	}
	return len(body) + 1
}

// sendChunk posts one chunk, halving it while the intake refuses it as too large.
func (d *Sender) sendChunk(ctx context.Context, events []map[string]any) error {
	if len(events) == 0 {
		return nil
	}
	body, err := json.Marshal(d.items(events))
	if err != nil {
		return fmt.Errorf("datadog: marshal batch: %w", err)
	}
	err = d.client.Post(ctx, body, "application/json")
	if isPayloadTooLarge(err) && len(events) > 1 {
		middle := len(events) / 2
		if err := d.sendChunk(ctx, events[:middle]); err != nil {
			return err
		}
		return d.sendChunk(ctx, events[middle:])
	}
	return err
}

// items maps every event to one log item.
func (d *Sender) items(events []map[string]any) []logItem {
	items := make([]logItem, 0, len(events))
	for _, event := range events {
		message, err := json.Marshal(event)
		if err != nil {
			message = []byte("{}")
		}
		items = append(items, logItem{
			DDSource: d.source,
			Service:  d.serviceName(event),
			DDTags:   d.datadogTags(event),
			Level:    stringOf(event["level"]),
			Message:  string(message),
		})
	}
	return items
}

// serviceName reads service.name, falling back to DD_SERVICE as it was at construction.
func (d *Sender) serviceName(event map[string]any) string {
	service, _ := event["service"].(map[string]any)
	if name := stringOf(service["name"]); name != "" {
		return name
	}
	return d.service
}

// datadogTags builds the comma-joined ddtags value from the event's service group.
// Values are absent when the event has none, except env, which falls back to DD_ENV as it
// was at construction.
func (d *Sender) datadogTags(event map[string]any) string {
	service, _ := event["service"].(map[string]any)
	env := stringOf(service["env"])
	if env == "" {
		env = d.env
	}
	var tags []string
	if env != "" {
		tags = append(tags, "env:"+env)
	}
	if version := stringOf(service["version"]); version != "" {
		tags = append(tags, "version:"+version)
	}
	if name := stringOf(service["name"]); name != "" {
		tags = append(tags, "service:"+name)
	}
	sort.Strings(tags)
	return strings.Join(tags, ",")
}

// stringOf reads a string value, treating a missing or wrong-typed value as "".
func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// isPayloadTooLarge reports whether err is a 413 from the intake.
func isPayloadTooLarge(err error) bool {
	var statusErr *httpdrain.StatusError
	return errors.As(err, &statusErr) && statusErr.Status == http.StatusRequestEntityTooLarge
}
