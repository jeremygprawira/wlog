// Package hyperdx sends wlog events to HyperDX over OTLP/HTTP JSON. It reuses the
// drain/otlp encoder, so one place fixes an OTLP bug. It reads HYPERDX_API_KEY,
// HYPERDX_ENDPOINT, and HYPERDX_SERVICE when an option does not set them.
package hyperdx

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/otlp"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// defaultEndpoint is HyperDX's OTLP intake.
const defaultEndpoint = "https://in-otel.hyperdx.io/v1/logs"

// config holds the resolved configuration.
type config struct {
	apiKey   string
	endpoint string
	service  string
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithAPIKey sets the HyperDX API key. Overrides HYPERDX_API_KEY.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithEndpoint sets the OTLP endpoint. Overrides HYPERDX_ENDPOINT.
func WithEndpoint(endpoint string) Option { return func(c *config) { c.endpoint = endpoint } }

// WithService sets the service resource attribute. Overrides HYPERDX_SERVICE.
func WithService(service string) Option { return func(c *config) { c.service = service } }

// Drain posts OTLP batches to HyperDX. It implements pipeline.Sender.
type Drain struct {
	client  *httpdrain.Client
	service string
}

// New builds a Drain from opts and the environment, wrapped for batching and retry.
func New(opts ...Option) (wlog.Drain, error) {
	c := config{
		apiKey:   os.Getenv("HYPERDX_API_KEY"),
		endpoint: os.Getenv("HYPERDX_ENDPOINT"),
		service:  os.Getenv("HYPERDX_SERVICE"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("hyperdx: HYPERDX_API_KEY is required")
	}
	if c.endpoint == "" {
		c.endpoint = defaultEndpoint
	}
	sender := &Drain{
		client: httpdrain.New(strings.TrimRight(c.endpoint, "/"),
			httpdrain.WithSource("hyperdx"),
			httpdrain.WithHeader("Authorization", c.apiKey),
		),
		service: c.service,
	}
	return pipeline.Wrap(sender), nil
}

// Must is New, but panics on a configuration error. Use it in main.
func Must(opts ...Option) wlog.Drain {
	drain, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return drain
}

// SendBatch encodes the batch with the shared OTLP encoder and posts it.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
	body, err := otlp.Encode(d.withService(events))
	if err != nil {
		return err
	}
	return d.client.Post(ctx, body, "application/json")
}

// withService overrides the service name on each event, when one was configured.
func (d *Drain) withService(events []map[string]any) []map[string]any {
	if d.service == "" {
		return events
	}
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		copied := make(map[string]any, len(event))
		for key, value := range event {
			copied[key] = value
		}
		service, _ := copied["service"].(map[string]any)
		replaced := make(map[string]any, len(service)+1)
		for key, value := range service {
			replaced[key] = value
		}
		replaced["name"] = d.service
		copied["service"] = replaced
		out = append(out, copied)
	}
	return out
}
