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
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
		gzip:         true,
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
	// A field from the environment can hold a space, and a space in the query makes every
	// request fail, so the names are trimmed and empty ones dropped before the check.
	fields := make([]string, 0, len(c.streamFields))
	for _, field := range c.streamFields {
		if field = strings.TrimSpace(field); field != "" {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		fields = defaultStreamFields
	}
	c.streamFields = fields
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

// SendBatch posts one line per event, drops a line over the cap with reason too_large, and
// halves a request the backend refuses with a 413.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var dropped []int
	kept := make([]map[string]any, 0, len(events))
	indexes := make([]int, 0, len(events))
	for i, event := range events {
		line, err := json.Marshal(event)
		if err != nil || len(line) > s.maxLine {
			dropped = append(dropped, i)
			continue
		}
		kept = append(kept, event)
		indexes = append(indexes, i)
	}
	reason := ""
	if len(kept) > 0 {
		d, why, err := s.post(ctx, kept, indexes)
		if err != nil {
			return err
		}
		dropped = append(dropped, d...)
		reason = why
	}
	if len(dropped) == 0 {
		return nil
	}
	if reason == "" {
		reason = "too_large"
	}
	return &pipeline.PartialError{Dropped: dropped, Reason: reason}
}

// post sends one run of events, and halves it while the backend refuses it as too large. A
// run of one event that still gets a 413 is dropped with reason too_large.
func (s *Sender) post(ctx context.Context, events []map[string]any, indexes []int) (dropped []int, reason string, err error) {
	var buf strings.Builder
	for i, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			dropped = append(dropped, indexes[i])
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	err = s.client.Post(ctx, []byte(buf.String()), "application/x-ndjson")
	if err == nil {
		return dropped, "", nil
	}
	var statusErr *httpdrain.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusRequestEntityTooLarge {
		return nil, "", err
	}
	if len(events) == 1 {
		return []int{indexes[0]}, "too_large", nil
	}
	half := len(events) / 2
	d1, why1, err := s.post(ctx, events[:half], indexes[:half])
	if err != nil {
		return nil, "", err
	}
	d2, why2, err := s.post(ctx, events[half:], indexes[half:])
	if err != nil {
		return nil, "", err
	}
	if why1 == "" {
		why1 = why2
	}
	return append(d1, d2...), why1, nil
}

// insertURL builds the jsonline endpoint with the field and stream parameters. The query
// goes through url.Values, so a field name is escaped instead of breaking the URL.
func insertURL(base string, streamFields []string) string {
	query := url.Values{
		"_msg_field":     {"summary"},
		"_time_field":    {"timestamp"},
		"_stream_fields": {strings.Join(streamFields, ",")},
	}
	return strings.TrimRight(base, "/") + "/insert/jsonline?" + query.Encode()
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
