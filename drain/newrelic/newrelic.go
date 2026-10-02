// Package newrelic sends wlog events to New Relic's log API. It reads
// NEW_RELIC_LICENSE_KEY or NEW_RELIC_API_KEY, and NEW_RELIC_REGION, when an option does
// not set them.
//
// New Relic answers one status per request, not one per event, and reports some failures
// later as NrIntegrationError events. This query finds them:
//
//	FROM NrIntegrationError SELECT timestamp, message, newRelicFeature, requestId WHERE newRelicFeature = 'Logging'
package newrelic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// maxAttributes is the attribute cap New Relic documents for one log.
const maxAttributes = 255

// regionEndpoints maps a region name to its log API host.
var regionEndpoints = map[string]string{
	"us":      "https://log-api.newrelic.com",
	"eu":      "https://log-api.eu.newrelic.com",
	"jp":      "https://log-api.jp.nr-data.net",
	"fedramp": "https://gov-log-api.newrelic.com",
}

// reservedUserKeys are user key names New Relic reserves. Each one moves under
// wlog.fields, so it never collides with a New Relic field.
var reservedUserKeys = map[string]bool{
	"accountId": true, "appId": true, "eventType": true,
	"entity.guid": true, "entity.name": true, "entity.type": true,
	"instrumentation.name": true, "instrumentation.provider": true,
	"instrumentation.version": true,
}

// attributeRenames maps one canonical path to the New Relic attribute name.
var attributeRenames = map[string]string{
	"trace.trace_id":   "trace.id",
	"trace.span_id":    "span.id",
	"error.kind":       "error.class",
	"service.instance": "hostname",
}

// config holds the resolved configuration.
type config struct {
	key          string
	region       string
	endpoint     string
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

// WithLicenseKey sets the New Relic license key. Overrides NEW_RELIC_LICENSE_KEY and
// NEW_RELIC_API_KEY.
func WithLicenseKey(key string) Option { return func(c *config) { c.key = key } }

// WithRegion sets the region: us, eu, jp, or fedramp. Overrides NEW_RELIC_REGION.
func WithRegion(region string) Option { return func(c *config) { c.region = region } }

// WithEndpoint sets the log API endpoint, for a proxy or a test. It wins over the region.
func WithEndpoint(endpoint string) Option { return func(c *config) { c.endpoint = endpoint } }

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

// Sender posts batches to New Relic. It implements pipeline.Sender.
type Sender struct {
	client *httpdrain.Client
	logger *wlog.Logger
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
		key:    firstEnv("NEW_RELIC_LICENSE_KEY", "NEW_RELIC_API_KEY"),
		region: os.Getenv("NEW_RELIC_REGION"),
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.key == "" {
		return nil, nil, fmt.Errorf("newrelic: NEW_RELIC_LICENSE_KEY is required")
	}
	endpoint, err := endpointOf(c)
	if err != nil {
		return nil, nil, err
	}
	clientOpts := []httpdrain.Option{
		httpdrain.WithSource("newrelic"),
		httpdrain.WithHeader("Api-Key", c.key),
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
	// The Log API lives under /log/v1. The root path answers nothing, so a drain that
	// posted there sent no event at all.
	return &Sender{client: httpdrain.New(endpoint+"/log/v1", clientOpts...)}, c.pipelineOpts, nil
}

// Setup keeps the Logger, so the attribute cap can report through it.
func (s *Sender) Setup(l *wlog.Logger) error {
	s.logger = l
	return nil
}

// SendBatch posts the events in envelopes that fit the Log API limit, and halves an
// envelope the API refuses with a 413. An event that alone passes the limit is dropped with
// reason too_large, because no request can carry it.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var retry, dropped []int
	reason := ""
	chunks, oversize := httpdrain.Chunks(events, 0, maxRequestBytes-envelopeBytes, s.logBytes)
	for _, i := range oversize {
		dropped = append(dropped, i)
		reason = "too_large"
	}
	for _, chunk := range chunks {
		post := func(ctx context.Context, chunkEvents []map[string]any) error {
			return s.postChunk(ctx, chunkEvents)
		}
		pe, err := httpdrain.SendChunk(ctx, events, chunk, post)
		switch {
		case err != nil:
			// The request failed as a whole: this chunk is retried or dropped, and the
			// chunks that landed are left alone.
			again := retryable(err)
			for i := chunk.Start; i < chunk.End; i++ {
				if again {
					retry = append(retry, i)
				} else {
					dropped = append(dropped, i)
				}
			}
			if reason == "" {
				reason = statusReason(err)
			}
		case pe != nil:
			dropped = append(dropped, pe.Dropped...)
			reason = pe.Reason
		}
	}
	if len(retry) == 0 && len(dropped) == 0 {
		return nil
	}
	return &pipeline.PartialError{Retry: retry, Dropped: dropped, Reason: reason}
}

// logBytes returns the encoded size of one event's log, with its separating comma.
func (s *Sender) logBytes(event map[string]any) int {
	item, _ := s.buildLog(event)
	body, err := json.Marshal(item)
	if err != nil {
		return 0
	}
	return len(body) + 1
}

// postChunk posts one envelope that holds the events.
func (s *Sender) postChunk(ctx context.Context, events []map[string]any) error {
	logs := make([]logItem, 0, len(events))
	for _, event := range events {
		logs = append(logs, s.logOf(event))
	}
	body, err := json.Marshal([]envelope{{Logs: logs}})
	if err != nil {
		return fmt.Errorf("newrelic: marshal batch: %w", err)
	}
	return s.client.Post(ctx, body, "application/json")
}

// envelope is the one-element array New Relic accepts.
type envelope struct {
	Logs []logItem `json:"logs"`
}

// logItem is one New Relic log.
type logItem struct {
	Timestamp  int64          `json:"timestamp"`
	Message    string         `json:"message"`
	Attributes map[string]any `json:"attributes"`
}

// logOf builds one New Relic log from an event.
func (s *Sender) logOf(event map[string]any) logItem {
	item, dropped := s.buildLog(event)
	if dropped > 0 {
		s.reportCap(dropped)
	}
	return item
}

// buildLog maps one event to a log, and reports how many attributes the cap dropped. The
// caller reports the cap once, so sizing an event does not report it twice.
func (s *Sender) buildLog(event map[string]any) (logItem, int) {
	attributes, dropped := attributesOf(event)
	return logItem{
		Timestamp:  timestampMillis(event),
		Message:    messageOf(event),
		Attributes: attributes,
	}, dropped
}

// attributesOf flattens an event to dotted keys, renames the New Relic fields, moves a
// reserved user key under wlog.fields, and keeps at most 255 attributes. It returns the
// count it dropped.
func attributesOf(event map[string]any) (map[string]any, int) {
	flat := map[string]any{}
	flatten(flat, "", event)
	out := make(map[string]any, len(flat))
	for key, value := range flat {
		name := key
		if renamed, ok := attributeRenames[key]; ok {
			name = renamed
		}
		if reservedUserKeys[key] {
			name = "wlog.fields." + key
		}
		out[name] = value
	}
	if len(out) <= maxAttributes {
		return out, 0
	}
	keys := make([]string, 0, len(out))
	for key := range out {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	dropped := len(keys) - maxAttributes
	for _, key := range keys[maxAttributes:] {
		delete(out, key)
	}
	return out, dropped
}

// flatten copies a nested map into dotted keys.
func flatten(out map[string]any, prefix string, value any) {
	nested, ok := value.(map[string]any)
	if !ok {
		if prefix != "" {
			out[prefix] = value
		}
		return
	}
	for key, child := range nested {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		flatten(out, path, child)
	}
}

// messageOf returns the summary as plain text.
func messageOf(event map[string]any) string {
	message, _ := event["summary"].(string)
	return message
}

// timestampMillis reads the event timestamp as epoch milliseconds.
func timestampMillis(event map[string]any) int64 {
	text, _ := event["timestamp"].(string)
	stamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Now().UnixMilli()
	}
	return stamp.UnixMilli()
}

// reportCap tells the caller that one log lost attributes to the cap.
func (s *Sender) reportCap(dropped int) {
	if s.logger == nil {
		return
	}
	s.logger.Report(wlog.Problem{
		Code:    "WLOG_CAP_REACHED",
		Source:  "newrelic",
		Message: "the attribute cap dropped attributes from a log",
		Count:   dropped,
	})
}

// maxRequestBytes is the Log API limit for one request. The docs count it before
// compression, because they do not say which the limit applies to.
const maxRequestBytes = 1_000_000

// envelopeBytes is the fixed part of the request body, which holds no log.
const envelopeBytes = len(`[{"logs":[]}]`)

// retryable reports whether a failed request is worth another try. A status the backend
// marks retryable, and a transport error, are retryable. Anything else is final.
func retryable(err error) bool {
	var statusErr *httpdrain.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Retryable()
	}
	return true
}

// statusReason names a failed request for the PartialError. It never holds an event value.
func statusReason(err error) string {
	var statusErr *httpdrain.StatusError
	if errors.As(err, &statusErr) {
		return "status_" + strconv.Itoa(statusErr.Status)
	}
	return "transport"
}

// endpointOf resolves the log API endpoint from the option, the region, or the default.
func endpointOf(c config) (string, error) {
	if c.endpoint != "" {
		return strings.TrimRight(c.endpoint, "/"), nil
	}
	region := strings.ToLower(strings.TrimSpace(c.region))
	if region == "" {
		region = "us"
	}
	host, ok := regionEndpoints[region]
	if !ok {
		return "", fmt.Errorf("newrelic: unknown region %q", c.region)
	}
	return host, nil
}

// firstEnv returns the first of the names that holds a value.
func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
