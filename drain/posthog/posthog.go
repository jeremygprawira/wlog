// Package posthog sends wlog events to PostHog's batch endpoint. It reads
// POSTHOG_API_KEY, POSTHOG_HOST, and WLOG_POSTHOG_EVENT when an option does not set them.
package posthog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// defaultHost is PostHog's US ingestion host.
const defaultHost = "https://us.i.posthog.com"

// defaultEvent is the PostHog event name when WLOG_POSTHOG_EVENT is unset.
const defaultEvent = "wlog_event"

// config holds the resolved configuration.
type config struct {
	apiKey string
	host   string
	event  string
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithAPIKey sets the PostHog project API key. Overrides POSTHOG_API_KEY.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithHost sets the PostHog ingestion host. Overrides POSTHOG_HOST.
func WithHost(host string) Option { return func(c *config) { c.host = host } }

// WithEvent sets the PostHog event name. Overrides WLOG_POSTHOG_EVENT.
func WithEvent(name string) Option { return func(c *config) { c.event = name } }

// Drain posts batches to PostHog. It implements pipeline.Sender.
type Drain struct {
	client *httpdrain.Client
	apiKey string
	event  string
}

// New builds a Drain from opts and the environment, wrapped for batching and retry.
func New(opts ...Option) (wlog.Drain, error) {
	c := config{
		apiKey: os.Getenv("POSTHOG_API_KEY"),
		host:   os.Getenv("POSTHOG_HOST"),
		event:  os.Getenv("WLOG_POSTHOG_EVENT"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("posthog: POSTHOG_API_KEY is required")
	}
	if c.host == "" {
		c.host = defaultHost
	}
	if c.event == "" {
		c.event = defaultEvent
	}
	sender := &Drain{
		client: httpdrain.New(strings.TrimRight(c.host, "/")+"/batch/", httpdrain.WithSource("posthog")),
		apiKey: c.apiKey,
		event:  c.event,
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

// SendBatch posts one batch object with a flattened property map per event.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
	batch := make([]map[string]any, 0, len(events))
	for _, event := range events {
		timestamp := event["timestamp"]
		if timestamp == nil {
			timestamp = time.Now().UTC().Format(time.RFC3339Nano)
		}
		batch = append(batch, map[string]any{
			"event":       d.event,
			"distinct_id": distinctID(event),
			"timestamp":   timestamp,
			"properties":  flatten(event, ""),
		})
	}
	body, err := json.Marshal(map[string]any{"api_key": d.apiKey, "batch": batch})
	if err != nil {
		return fmt.Errorf("posthog: marshal batch: %w", err)
	}
	return d.client.Post(ctx, body, "application/json")
}

// distinctID reads user.id first, then trace.request_id.
func distinctID(event map[string]any) string {
	if user, ok := event["user"].(map[string]any); ok {
		if id, _ := user["id"].(string); id != "" {
			return id
		}
	}
	if trace, ok := event["trace"].(map[string]any); ok {
		if id, _ := trace["request_id"].(string); id != "" {
			return id
		}
	}
	return "anonymous"
}

// flatten turns a nested event into dotted keys, because PostHog charts a flat property
// map and handles a nested object poorly.
func flatten(value map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for key, item := range value {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if nested, ok := item.(map[string]any); ok {
			for nestedKey, nestedValue := range flatten(nested, path) {
				out[nestedKey] = nestedValue
			}
			continue
		}
		out[path] = item
	}
	return out
}
