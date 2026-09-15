// Package otlp sends wlog events to an OpenTelemetry collector over OTLP/HTTP JSON. It
// reads OTEL_EXPORTER_OTLP_ENDPOINT and OTEL_EXPORTER_OTLP_HEADERS when an option does
// not set them.
package otlp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog/internal/httpdrain"
)

// defaultEndpoint is a local collector with the default OTLP/HTTP port.
const defaultEndpoint = "http://localhost:4318"

// config holds the resolved configuration for one Drain.
type config struct {
	endpoint  string
	headers   map[string]string
	headerSet bool
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithEndpoint sets the collector base URL. The drain appends /v1/logs. Overrides
// OTEL_EXPORTER_OTLP_ENDPOINT. The default is http://localhost:4318.
func WithEndpoint(endpoint string) Option { return func(c *config) { c.endpoint = endpoint } }

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

// Drain posts batches to one collector. It implements pipeline.Sender, so wrap it with
// pipeline.Wrap to get batching, retry, and a bounded buffer.
type Drain struct {
	client *httpdrain.Client
}

// New builds a Drain from opts and the OTEL_EXPORTER_OTLP_* env vars.
func New(opts ...Option) (*Drain, error) {
	c := config{endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")}
	headers := parseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))
	for _, opt := range opts {
		opt(&c)
	}
	if c.endpoint == "" {
		c.endpoint = defaultEndpoint
	}
	if c.headerSet {
		headers = c.headers
	}

	url := strings.TrimRight(c.endpoint, "/") + "/v1/logs"
	clientOpts := []httpdrain.Option{httpdrain.WithSource("otlp")}
	for k, v := range headers {
		clientOpts = append(clientOpts, httpdrain.WithHeader(k, v))
	}
	return &Drain{client: httpdrain.New(url, clientOpts...)}, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) *Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// parseHeaders reads the OTEL_EXPORTER_OTLP_HEADERS form: comma-separated key=value
// pairs. A pair without an equals sign is skipped.
func parseHeaders(raw string) map[string]string {
	headers := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		headers[key] = strings.TrimSpace(value)
	}
	return headers
}

// SendBatch maps every event to one log record and posts one request. Events are
// grouped by resource, so a batch with two services still reports each service's own
// attributes.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
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
		return fmt.Errorf("otlp: marshal request: %w", err)
	}
	return d.client.Post(ctx, body, "application/json")
}
