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

	"github.com/jeremygprawira/wlog/internal/httpdrain"
)

// Defaults for the Datadog US site and the wlog source tag.
const (
	defaultSite   = "datadoghq.com"
	defaultSource = "wlog"
)

// config holds the resolved configuration for one Drain.
type config struct {
	apiKey string
	site   string
	url    string
	source string
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

// Drain posts batches to the Datadog logs intake. It implements pipeline.Sender, so
// wrap it with pipeline.Wrap to get batching, retry, and a bounded buffer.
type Drain struct {
	client *httpdrain.Client
	source string
}

// New builds a Drain from opts and the DD_* env vars. It returns an error when the API
// key is missing.
func New(opts ...Option) (*Drain, error) {
	c := config{
		apiKey: os.Getenv("DD_API_KEY"),
		site:   os.Getenv("DD_SITE"),
		source: defaultSource,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.apiKey == "" {
		return nil, errors.New("datadog: DD_API_KEY is required")
	}
	if c.site == "" {
		c.site = defaultSite
	}
	url := c.url
	if url == "" {
		url = intakeURL(c.site)
	}
	client := httpdrain.New(url,
		httpdrain.WithSource("datadog"),
		httpdrain.WithHeader("DD-API-KEY", c.apiKey),
	)
	return &Drain{client: client, source: c.source}, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) *Drain {
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

// SendBatch posts the events as one JSON array. A 413 response splits the batch in half
// and sends each half again, so a large batch still lands.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
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
		if err := d.SendBatch(ctx, events[:middle]); err != nil {
			return err
		}
		return d.SendBatch(ctx, events[middle:])
	}
	return err
}

// items maps every event to one log item.
func (d *Drain) items(events []map[string]any) []logItem {
	items := make([]logItem, 0, len(events))
	for _, event := range events {
		message, err := json.Marshal(event)
		if err != nil {
			message = []byte("{}")
		}
		items = append(items, logItem{
			DDSource: d.source,
			Service:  serviceName(event),
			DDTags:   datadogTags(event),
			Level:    stringOf(event["level"]),
			Message:  string(message),
		})
	}
	return items
}

// serviceName reads service.name, falling back to DD_SERVICE.
func serviceName(event map[string]any) string {
	service, _ := event["service"].(map[string]any)
	if name := stringOf(service["name"]); name != "" {
		return name
	}
	return os.Getenv("DD_SERVICE")
}

// datadogTags builds the comma-joined ddtags value from the event's service group.
// Values are absent when the event has none, except env, which falls back to DD_ENV.
func datadogTags(event map[string]any) string {
	service, _ := event["service"].(map[string]any)
	env := stringOf(service["env"])
	if env == "" {
		env = os.Getenv("DD_ENV")
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
