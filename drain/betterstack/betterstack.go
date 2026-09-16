// Package betterstack sends wlog events to Better Stack's logs intake. It reads
// BETTERSTACK_SOURCE_TOKEN and BETTERSTACK_HOST when an option does not set them.
package betterstack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/httpdrain"
	"github.com/jeremygprawira/wlog/pipeline"
)

// defaultHost is Better Stack's intake host.
const defaultHost = "https://in.logs.betterstack.com"

// config holds the resolved configuration.
type config struct {
	token string
	host  string
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithSourceToken sets the source token. Overrides BETTERSTACK_SOURCE_TOKEN.
func WithSourceToken(token string) Option { return func(c *config) { c.token = token } }

// WithHost sets the intake host. Overrides BETTERSTACK_HOST.
func WithHost(host string) Option { return func(c *config) { c.host = host } }

// Drain posts batches to Better Stack. It implements pipeline.Sender.
type Drain struct {
	client *httpdrain.Client
}

// New builds a Drain from opts and the environment, wrapped for batching and retry.
func New(opts ...Option) (wlog.Drain, error) {
	c := config{token: os.Getenv("BETTERSTACK_SOURCE_TOKEN"), host: os.Getenv("BETTERSTACK_HOST")}
	for _, opt := range opts {
		opt(&c)
	}
	if c.token == "" {
		return nil, fmt.Errorf("betterstack: BETTERSTACK_SOURCE_TOKEN is required")
	}
	if c.host == "" {
		c.host = defaultHost
	}
	sender := &Drain{client: httpdrain.New(strings.TrimRight(c.host, "/"),
		httpdrain.WithSource("betterstack"),
		httpdrain.WithHeader("Authorization", "Bearer "+c.token),
	)}
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

// SendBatch posts one JSON array. The event goes over as it is, nested, with dt, level,
// and message mapped so Better Stack's own columns line up.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
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
