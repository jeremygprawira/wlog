// Package clickhouse sends wlog events to ClickHouse over its HTTP interface as
// JSONEachRow. It reads CLICKHOUSE_URL, CLICKHOUSE_USER, CLICKHOUSE_PASSWORD,
// CLICKHOUSE_DATABASE, and CLICKHOUSE_TABLE when an option does not set them.
package clickhouse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// timestampFormat is the DateTime64(9) text form ClickHouse accepts under the default
// input format.
const timestampFormat = "2006-01-02 15:04:05.000000000"

// identifierPattern is the only shape a database or table name may take, so a config
// value can never inject SQL.
var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Defaults for a local ClickHouse.
const (
	defaultURL      = "http://localhost:8123"
	defaultDatabase = "default"
	defaultTable    = "wlog_events"
)

// config holds the resolved configuration for one Sender.
type config struct {
	url          string
	user         string
	password     string
	database     string
	table        string
	pipelineOpts []pipeline.Option
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

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender inserts batches into one ClickHouse table. It implements pipeline.Sender. It
// never creates the table: run DDL first.
type Sender struct {
	client *httpdrain.Client
}

// New returns the ClickHouse drain with the pipeline defaults, or with the options
// WithPipeline set.
func New(opts ...Option) (wlog.Drain, error) {
	s, popts, err := newSender(opts...)
	if err != nil {
		return nil, err
	}
	return pipeline.Wrap(s, popts...), nil
}

// NewSender returns the raw sender, for a caller that builds its own pipeline or sends a
// batch itself.
func NewSender(opts ...Option) (*Sender, error) {
	s, _, err := newSender(opts...)
	return s, err
}

// newSender resolves one configuration, so New and NewSender can never disagree.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
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
	// A config value that reaches SQL is a trust boundary, so both names must be plain
	// identifiers. Quoting would be another way to do it, and this is the stricter one.
	if !identifierPattern.MatchString(c.database) {
		return nil, nil, fmt.Errorf("clickhouse: database %q must match %s", c.database, identifierPattern)
	}
	if !identifierPattern.MatchString(c.table) {
		return nil, nil, fmt.Errorf("clickhouse: table %q must match %s", c.table, identifierPattern)
	}

	parsed, err := url.Parse(c.url)
	if err != nil {
		return nil, nil, fmt.Errorf("clickhouse: CLICKHOUSE_URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, nil, errors.New("clickhouse: CLICKHOUSE_URL needs a scheme and a host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
	// The URL may already carry its own settings, such as secure=true, so the insert
	// statement joins them instead of replacing them.
	query := parsed.Query()
	query.Set("query", fmt.Sprintf("INSERT INTO %s.%s FORMAT JSONEachRow", c.database, c.table))
	parsed.RawQuery = query.Encode()

	clientOpts := []httpdrain.Option{httpdrain.WithSource("clickhouse")}
	if c.user != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(c.user + ":" + c.password))
		clientOpts = append(clientOpts, httpdrain.WithHeader("Authorization", "Basic "+auth))
	}
	return &Sender{client: httpdrain.New(parsed.String(), clientOpts...)}, c.pipelineOpts, nil
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// SendBatch inserts the events as one JSONEachRow body.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var buf strings.Builder
	for _, event := range events {
		row, err := json.Marshal(rowFor(event))
		if err != nil {
			return fmt.Errorf("clickhouse: marshal row: %w", err)
		}
		buf.Write(row)
		buf.WriteByte('\n')
	}
	return s.client.Post(ctx, []byte(buf.String()), "application/x-ndjson")
}

// rowFor maps one event to one row: the reserved fields as columns, plus the whole
// event in the event JSON column. A missing value becomes the column's zero value.
func rowFor(event map[string]any) map[string]any {
	service, _ := event["service"].(map[string]any)
	trace, _ := event["trace"].(map[string]any)
	httpFields, _ := event["http"].(map[string]any)
	errInfo, _ := event["error"].(map[string]any)
	return map[string]any{
		"timestamp":       clickHouseTime(event["timestamp"]),
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

// clickHouseTime renders an RFC 3339 timestamp in UTC as DateTime64(9) text, which is what
// ClickHouse accepts under its default input format. A missing or unparsable value
// becomes the zero time rather than a rejected row.
func clickHouseTime(value any) string {
	text, _ := value.(string)
	if text != "" {
		if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return t.UTC().Format(timestampFormat)
		}
	}
	return time.Time{}.UTC().Format(timestampFormat)
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
