// Package victorialogs sends wlog events to VictoriaLogs' jsonline endpoint. It reads
// VICTORIALOGS_URL, VICTORIALOGS_STREAM_FIELDS, VICTORIALOGS_ACCOUNT_ID, and
// VICTORIALOGS_PROJECT_ID when an option does not set them.
//
// VictoriaLogs never reports a bad line to the client: it skips a line it cannot read,
// and fails the request only when every line fails. So the drain sends only lines it
// encoded, and it drops a line over the size cap itself, because the server skips such a
// line with no error.
package victorialogs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// Defaults the spec names.
const (
	defaultMaxLineBytes = 256 * 1024
)

// defaultStreamFields are the low-cardinality fields VictoriaLogs streams on.
var defaultStreamFields = []string{"service.name", "service.env"}

// config holds the resolved configuration.
type config struct {
	url          string
	streamFields []string
	maxLine      int
	accountID    string
	projectID    string
	httpClient   *http.Client
	timeout      time.Duration
	timeoutSet   bool
	userAgent    string
	userAgentSet bool
	gzip         bool
	pipelineOpts []pipeline.Option
}

// Option sets one config value. An option always wins over the matching env var.
type Option func(*config)

// WithURL sets the VictoriaLogs URL. Overrides VICTORIALOGS_URL.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithStreamFields sets the fields VictoriaLogs streams on. New rejects a
// high-cardinality field.
func WithStreamFields(fields ...string) Option {
	return func(c *config) {
		if len(fields) > 0 {
			c.streamFields = append([]string(nil), fields...)
		}
	}
}

// WithMaxLineBytes sets the line cap. Default 256 KiB.
func WithMaxLineBytes(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxLine = n
		}
	}
}

// WithTenant sends the AccountID and ProjectID headers. Without it, the drain sends
// neither.
func WithTenant(accountID, projectID string) Option {
	return func(c *config) {
		c.accountID = accountID
		c.projectID = projectID
	}
}

// WithHTTPClient uses a caller-supplied HTTP client. wlog never changes it.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithTimeout sets the HTTP request timeout. Default 10s.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
		c.timeoutSet = true
	}
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *config) {
		c.userAgent = ua
		c.userAgentSet = true
	}
}

// WithGzip compresses the body when on.
func WithGzip(on bool) Option { return func(c *config) { c.gzip = on } }

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender posts jsonline bodies to one VictoriaLogs. It implements pipeline.Sender.
type Sender struct {
	client  *httpdrain.Client
	maxLine int
}

// New returns the drain with the pipeline defaults, or with the options WithPipeline set.
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

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(opts ...Option) wlog.Drain {
	d, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// newSender resolves one configuration from opts and the environment.
func newSender(opts ...Option) (*Sender, []pipeline.Option, error) {
	c := config{
		url:          os.Getenv("VICTORIALOGS_URL"),
		streamFields: splitFields(os.Getenv("VICTORIALOGS_STREAM_FIELDS")),
		maxLine:      defaultMaxLineBytes,
		accountID:    os.Getenv("VICTORIALOGS_ACCOUNT_ID"),
		projectID:    os.Getenv("VICTORIALOGS_PROJECT_ID"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.url == "" {
		return nil, nil, fmt.Errorf("victorialogs: VICTORIALOGS_URL is required")
	}
	if len(c.streamFields) == 0 {
		c.streamFields = defaultStreamFields
	}
	for _, field := range c.streamFields {
		if highCardinality(field) {
			return nil, nil, fmt.Errorf("victorialogs: stream field %q has high cardinality", field)
		}
	}
	clientOpts := []httpdrain.Option{httpdrain.WithSource("victorialogs")}
	if c.accountID != "" {
		clientOpts = append(clientOpts, httpdrain.WithHeader("AccountID", c.accountID))
	}
	if c.projectID != "" {
		clientOpts = append(clientOpts, httpdrain.WithHeader("ProjectID", c.projectID))
	}
	if c.httpClient != nil {
		clientOpts = append(clientOpts, httpdrain.WithHTTPClient(c.httpClient))
	}
	if c.timeoutSet {
		clientOpts = append(clientOpts, httpdrain.WithTimeout(c.timeout))
	}
	if c.userAgentSet {
		clientOpts = append(clientOpts, httpdrain.WithUserAgent(c.userAgent))
	}
	if c.gzip {
		clientOpts = append(clientOpts, httpdrain.WithGzip(true))
	}
	return &Sender{
		client:  httpdrain.New(insertURL(c.url, c.streamFields), clientOpts...),
		maxLine: c.maxLine,
	}, c.pipelineOpts, nil
}

// SendBatch posts one line per event, and drops a line over the cap with reason too_large.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var dropped []int
	var buf strings.Builder
	for i, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			dropped = append(dropped, i)
			continue
		}
		if len(line) > s.maxLine {
			dropped = append(dropped, i)
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if buf.Len() > 0 {
		if err := s.client.Post(ctx, []byte(buf.String()), "application/x-ndjson"); err != nil {
			return err
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	return &pipeline.PartialError{Dropped: dropped, Reason: "too_large"}
}

// insertURL builds the jsonline endpoint with the field and stream parameters.
func insertURL(base string, streamFields []string) string {
	return strings.TrimRight(base, "/") + "/insert/jsonline" +
		"?_msg_field=summary&_time_field=timestamp&_stream_fields=" + strings.Join(streamFields, ",")
}

// splitFields splits a comma-separated field list, trimming spaces.
func splitFields(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if field := strings.TrimSpace(part); field != "" {
			out = append(out, field)
		}
	}
	return out
}

// highCardinality reports whether a stream field can hold one value per event, which
// VictoriaLogs refuses.
func highCardinality(field string) bool {
	switch field {
	case "event_id", "http.path", "http.client_ip":
		return true
	}
	return strings.HasPrefix(field, "trace.") || strings.HasPrefix(field, "user.")
}
