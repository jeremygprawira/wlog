// Package clickhouse sends wlog events to ClickHouse over its HTTP interface as
// JSONEachRow. It reads CLICKHOUSE_URL, CLICKHOUSE_USER, CLICKHOUSE_PASSWORD,
// CLICKHOUSE_DATABASE, and CLICKHOUSE_TABLE when an option does not set them.
package clickhouse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jeremygprawira/wlog/internal/httpdrain"
)

// Defaults for a local ClickHouse.
const (
	defaultURL      = "http://localhost:8123"
	defaultDatabase = "default"
	defaultTable    = "wlog_events"
)

// config holds the resolved configuration for one Drain.
type config struct {
	url      string
	user     string
	password string
	database string
	table    string
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithURL sets the ClickHouse HTTP URL. Overrides CLICKHOUSE_URL.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithBasicAuth sets the ClickHouse username and password.
func WithBasicAuth(user, password string) Option {
	return func(c *config) { c.user, c.password = user, password }
}

// WithDatabase sets the database. Overrides CLICKHOUSE_DATABASE.
func WithDatabase(database string) Option { return func(c *config) { c.database = database } }

// WithTable sets the table. Overrides CLICKHOUSE_TABLE.
func WithTable(table string) Option { return func(c *config) { c.table = table } }

// Drain inserts batches into one ClickHouse table. It implements pipeline.Sender, so
// wrap it with pipeline.Wrap to get batching, retry, and a bounded buffer. It never
// creates the table: run DDL first.
type Drain struct {
	client *httpdrain.Client
}

// New builds a Drain from opts and the CLICKHOUSE_* env vars.
func New(opts ...Option) (*Drain, error) {
	c := config{
		url:      os.Getenv("CLICKHOUSE_URL"),
		user:     os.Getenv("CLICKHOUSE_USER"),
		password: os.Getenv("CLICKHOUSE_PASSWORD"),
		database: os.Getenv("CLICKHOUSE_DATABASE"),
		table:    os.Getenv("CLICKHOUSE_TABLE"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.url == "" {
		c.url = defaultURL
	}
	if c.database == "" {
		c.database = defaultDatabase
	}
	if c.table == "" {
		c.table = defaultTable
	}

	base := strings.TrimRight(c.url, "/")
	query := url.Values{}
	query.Set("query", fmt.Sprintf("INSERT INTO %s.%s FORMAT JSONEachRow", c.database, c.table))
	endpoint := base + "/?" + query.Encode()

	clientOpts := []httpdrain.Option{httpdrain.WithSource("clickhouse")}
	if c.user != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(c.user + ":" + c.password))
		clientOpts = append(clientOpts, httpdrain.WithHeader("Authorization", "Basic "+auth))
	}
	return &Drain{client: httpdrain.New(endpoint, clientOpts...)}, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) *Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// SendBatch inserts the events as one JSONEachRow body.
func (d *Drain) SendBatch(ctx context.Context, events []map[string]any) error {
	var buf strings.Builder
	for _, event := range events {
		row, err := json.Marshal(rowFor(event))
		if err != nil {
			return fmt.Errorf("clickhouse: marshal row: %w", err)
		}
		buf.Write(row)
		buf.WriteByte('\n')
	}
	return d.client.Post(ctx, []byte(buf.String()), "application/x-ndjson")
}

// rowFor maps one event to one row: the reserved fields as columns, plus the whole
// event in the event JSON column. A missing value becomes the column's zero value.
func rowFor(event map[string]any) map[string]any {
	service, _ := event["service"].(map[string]any)
	trace, _ := event["trace"].(map[string]any)
	httpFields, _ := event["http"].(map[string]any)
	errInfo, _ := event["error"].(map[string]any)
	return map[string]any{
		"timestamp":       stringOf(event["timestamp"]),
		"level":           stringOf(event["level"]),
		"operation":       stringOf(event["operation"]),
		"duration_ms":     numberOrZero(event["duration_ms"]),
		"outcome":         stringOf(event["outcome"]),
		"service_name":    stringOf(service["name"]),
		"service_version": stringOf(service["version"]),
		"service_env":     stringOf(service["env"]),
		"trace_id":        stringOf(trace["trace_id"]),
		"span_id":         stringOf(trace["span_id"]),
		"request_id":      stringOf(trace["request_id"]),
		"http_method":     stringOf(httpFields["method"]),
		"http_route":      stringOf(httpFields["route"]),
		"http_status":     numberOrZero(httpFields["status"]),
		"error_code":      stringOf(errInfo["code"]),
		"error_message":   stringOf(errInfo["message"]),
		"event":           event,
	}
}

// stringOf reads a string value, treating a missing or wrong-typed value as "".
func stringOf(value any) string {
	text, _ := value.(string)
	return text
}

// numberOrZero reads a number, treating a missing or wrong-typed value as 0. A float
// with a zero fraction is returned as an integer, so the JSON has no decimal point.
func numberOrZero(value any) any {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return number
	case float64:
		if number == float64(int64(number)) {
			return int64(number)
		}
		return number
	default:
		return 0
	}
}
