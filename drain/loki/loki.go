// Package loki sends wlog events to Loki's Push API as JSON streams. It reads
// LOKI_URL, LOKI_USERNAME, LOKI_PASSWORD, and LOKI_TENANT_ID when an option does not
// set them.
package loki

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// defaultURL is a local Loki, the common development setup.
const defaultURL = "http://localhost:3100"

// defaultLabels is the label set used when WithLabels is not given. The set stays
// small on purpose, so one service does not create unbounded streams.
var defaultLabels = []string{"service", "env", "level"}

// highCardinalityKeys are keys that must never become a Loki label. Each one has a
// different value per request, so using it as a label would explode the stream count.
var highCardinalityKeys = []string{
	"trace.request_id",
	"trace.trace_id",
	"trace.span_id",
	"http.path",
	"http.client_ip",
	"http.user_agent",
	"error.message",
	"user.id",
}

// config holds the resolved configuration for one Drain.
type config struct {
	url      string
	username string
	password string
	tenantID string
	labels   []string
	labelSet bool
	gzip     bool
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithURL sets the Loki base URL. Overrides LOKI_URL. The default is
// http://localhost:3100.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithBasicAuth sets the username and password sent as basic auth.
func WithBasicAuth(username, password string) Option {
	return func(c *config) { c.username, c.password = username, password }
}

// WithTenantID sets the X-Scope-OrgID header. Overrides LOKI_TENANT_ID.
func WithTenantID(id string) Option { return func(c *config) { c.tenantID = id } }

// WithLabels replaces the default label set. A high-cardinality key makes New fail.
func WithLabels(keys ...string) Option {
	return func(c *config) { c.labels, c.labelSet = keys, true }
}

// WithGzip compresses the request body. Off by default.
func WithGzip(on bool) Option { return func(c *config) { c.gzip = on } }

// Drain sends batches to one Loki instance. It implements pipeline.Sender, so wrap it
// with pipeline.Wrap to get batching, retry, and a bounded buffer.
type Drain struct {
	client *httpdrain.Client
	labels []string
}

// New builds a Drain from opts and the LOKI_* env vars. It returns an error when a
// configured label is high-cardinality, so a bad label fails at startup instead of
// overloading Loki later.
func New(opts ...Option) (*Drain, error) {
	c := config{
		url:      os.Getenv("LOKI_URL"),
		username: os.Getenv("LOKI_USERNAME"),
		password: os.Getenv("LOKI_PASSWORD"),
		tenantID: os.Getenv("LOKI_TENANT_ID"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.url == "" {
		c.url = defaultURL
	}
	c.url = strings.TrimRight(c.url, "/")

	labels := c.labels
	if !c.labelSet {
		labels = defaultLabels
	}
	for _, key := range labels {
		if isHighCardinality(key) {
			return nil, fmt.Errorf("loki: label %q is high-cardinality and not allowed", key)
		}
	}

	clientOpts := []httpdrain.Option{
		httpdrain.WithGzip(c.gzip),
		httpdrain.WithSource("loki"),
	}
	if c.username != "" || c.password != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(c.username + ":" + c.password))
		clientOpts = append(clientOpts, httpdrain.WithHeader("Authorization", "Basic "+auth))
	}
	if c.tenantID != "" {
		clientOpts = append(clientOpts, httpdrain.WithHeader("X-Scope-OrgID", c.tenantID))
	}
	return &Drain{
		client: httpdrain.New(c.url+"/loki/api/v1/push", clientOpts...),
		labels: labels,
	}, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) *Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// isHighCardinality reports whether key is on the denylist.
func isHighCardinality(key string) bool {
	for _, denied := range highCardinalityKeys {
		if key == denied {
			return true
		}
	}
	return false
}

// stream is one Loki stream: one label set and its timestamped lines.
type stream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

// SendBatch groups the events by label set and posts one Push API request. Events with
// the same labels share a stream, so one batch can produce several streams.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
	groups := map[string]*stream{}
	for _, event := range events {
		labels := d.labelValues(event)
		key := canonicalLabels(labels)
		s := groups[key]
		if s == nil {
			s = &stream{Stream: labels}
			groups[key] = s
		}
		line, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("loki: marshal event: %w", err)
		}
		s.Values = append(s.Values, [2]string{timestampNanos(event), string(line)})
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	streams := make([]*stream, 0, len(keys))
	for _, key := range keys {
		streams = append(streams, groups[key])
	}

	body, err := json.Marshal(struct {
		Streams []*stream `json:"streams"`
	}{Streams: streams})
	if err != nil {
		return fmt.Errorf("loki: marshal push body: %w", err)
	}
	return d.client.Post(ctx, body, "application/json")
}

// labelValues reads one value per configured label from the event. A missing value is
// the empty string, which Loki accepts.
func (d *Drain) labelValues(event map[string]any) map[string]string {
	values := make(map[string]string, len(d.labels))
	for _, key := range d.labels {
		values[key] = labelValue(event, key)
	}
	return values
}

// labelValue reads one label. service and env come from the event's service group, so
// the common labels need no extra fields. Every other label is a top-level key.
func labelValue(event map[string]any, key string) string {
	if key == "service" || key == "env" {
		service, ok := event["service"].(map[string]any)
		if !ok {
			return ""
		}
		field := "name"
		if key == "env" {
			field = "env"
		}
		value, _ := service[field].(string)
		return value
	}
	switch value := event[key].(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}

// canonicalLabels makes one stable key for a label set, so the same labels always land
// in the same stream and the streams array has a deterministic order.
func canonicalLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(labels[key])
		b.WriteByte(0)
	}
	return b.String()
}

// timestampNanos reads the event's own timestamp as Unix nanoseconds. Loki wants
// nanosecond precision as a decimal string. A missing or bad timestamp uses now.
func timestampNanos(event map[string]any) string {
	if text, ok := event["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return strconv.FormatInt(t.UnixNano(), 10)
		}
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
