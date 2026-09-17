// Package otlp sends wlog events to an OpenTelemetry collector over OTLP/HTTP JSON. It
// reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_EXPORTER_OTLP_HEADERS when an option does
// not set them.
package otlp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// defaultEndpoint is a local collector with the default OTLP/HTTP port.
const defaultEndpoint = "http://localhost:4318"

// config holds the resolved configuration for one Sender.
type config struct {
	endpoint     string
	logsEndpoint string
	logsSet      bool
	headers      map[string]string
	headerSet    bool
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithEndpoint sets the collector base URL. The drain appends /v1/logs, unless
// OTEL_EXPORTER_OTLP_LOGS_ENDPOINT or WithLogsEndpoint names the full logs URL. Overrides
// OTEL_EXPORTER_OTLP_ENDPOINT. The default is http://localhost:4318.
func WithEndpoint(endpoint string) Option { return func(c *config) { c.endpoint = endpoint } }

// WithLogsEndpoint sets the complete logs URL, used as it stands. Overrides
// OTEL_EXPORTER_OTLP_LOGS_ENDPOINT, which the OpenTelemetry spec defines as the full URL.
func WithLogsEndpoint(endpoint string) Option {
	return func(c *config) { c.logsEndpoint, c.logsSet = endpoint, true }
}

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// WithHeaders replaces the OTEL_EXPORTER_OTLP_HEADERS set.
func WithHeaders(headers map[string]string) Option {
	return func(c *config) {
		c.headers = map[string]string{}
		for k, v := range headers {
			c.headers[k] = v
		}
		c.headerSet = true
	}
}

// Sender posts batches to one collector. It implements pipeline.Sender.
type Sender struct {
	client *httpdrain.Client
}

// New returns the OTLP drain with the pipeline defaults, or with the options WithPipeline
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

// newSender resolves one configuration from the options and the OTEL_EXPORTER_OTLP_* env
// vars.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		endpoint:     os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		logsEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"),
	}
	headers := parseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))
	for _, opt := range opts {
		opt(&c)
	}
	if c.endpoint == "" {
		c.endpoint = defaultEndpoint
	}
	if c.logsEndpoint == "" {
		c.logsEndpoint = strings.TrimRight(c.endpoint, "/") + "/v1/logs"
	}
	if c.headerSet {
		headers = c.headers
	}

	clientOpts := []httpdrain.Option{httpdrain.WithSource("otlp")}
	for k, v := range headers {
		clientOpts = append(clientOpts, httpdrain.WithHeader(k, v))
	}
	return &Sender{client: httpdrain.New(c.logsEndpoint, clientOpts...)}, c.pipelineOpts, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// parseHeaders reads the OTEL_EXPORTER_OTLP_HEADERS form: comma-separated key=value
// pairs, with both sides percent-encoded as the OpenTelemetry spec requires. A pair
// without an equals sign is skipped, because it names no header.
func parseHeaders(raw string) map[string]string {
	headers := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = decodeComponent(key)
		if key == "" {
			continue
		}
		headers[key] = decodeComponent(value)
	}
	return headers
}

// decodeComponent percent-decodes one header part. A part that cannot be decoded is kept
// as it stands, so a stray percent sign never drops a header.
func decodeComponent(part string) string {
	trimmed := strings.TrimSpace(part)
	if decoded, err := url.QueryUnescape(trimmed); err == nil {
		return decoded
	}
	return trimmed
}

// SendBatch maps every event to one log record and posts one request.
func (d *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	body, err := Encode(events)
	if err != nil {
		return err
	}
	return d.client.Post(ctx, body, "application/json")
}

// Encode renders the OTLP/HTTP JSON request body for a batch. Events are grouped by
// resource, so a batch with two services still reports each service's own attributes.
// A sibling drain that speaks OTLP reuses this encoder.
func Encode(events []map[string]any) ([]byte, error) {
	groups := map[string]*resourceLogs{}
	order := []string{}
	for _, event := range events {
		r := resourceFor(event)
		key := canonicalResource(r)
		group := groups[key]
		if group == nil {
			group = &resourceLogs{
				Resource:  r,
				ScopeLogs: []scopeLogs{{Scope: scopeFor()}},
			}
			groups[key] = group
			order = append(order, key)
		}
		group.ScopeLogs[0].LogRecords = append(group.ScopeLogs[0].LogRecords, recordFor(event))
	}
	sort.Strings(order)

	request := exportRequest{ResourceLogs: make([]resourceLogs, 0, len(order))}
	for _, key := range order {
		request.ResourceLogs = append(request.ResourceLogs, *groups[key])
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("otlp: marshal request: %w", err)
	}
	return body, nil
}
