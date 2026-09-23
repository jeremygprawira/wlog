// Package splunk sends wlog events to Splunk's HTTP Event Collector. It reads
// SPLUNK_HEC_URL, SPLUNK_HEC_TOKEN, SPLUNK_INDEX, and SPLUNK_SOURCETYPE when an option
// does not set them.
//
// Every request carries one random channel id, so a token with indexer acknowledgement
// on also accepts events. The drain never polls acknowledgements.
//
// Splunk answers one code per request. Code 6 means one event in the batch is bad, and
// earlier events can already be indexed, so the drain splits the batch in half and sends
// each half once.
package splunk

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/pipeline/httpdrain"
)

// Defaults the spec names.
const (
	defaultSource     = "wlog"
	defaultSourceType = "_json"
	defaultMaxBatch   = 1 << 20
)

// retryCodes are the HEC codes worth another try. Code 6 is handled by a split instead.
var retryCodes = map[int]bool{9: true, 18: true, 19: true, 20: true, 23: true, 26: true, 27: true}

// config holds the resolved configuration.
type config struct {
	url          string
	token        string
	index        string
	source       string
	sourceType   string
	maxBatch     int
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

// WithURL sets the collector URL. Overrides SPLUNK_HEC_URL.
func WithURL(url string) Option { return func(c *config) { c.url = url } }

// WithToken sets the HEC token. Overrides SPLUNK_HEC_TOKEN.
func WithToken(token string) Option { return func(c *config) { c.token = token } }

// WithIndex sets the target index. Overrides SPLUNK_INDEX. Empty omits the index.
func WithIndex(index string) Option { return func(c *config) { c.index = index } }

// WithSource sets the source field. Default wlog.
func WithSource(source string) Option { return func(c *config) { c.source = source } }

// WithSourceType sets the sourcetype field. Default _json.
func WithSourceType(sourcetype string) Option { return func(c *config) { c.sourceType = sourcetype } }

// WithMaxBatchBytes sets the body cap before compression. Default 1 MiB.
func WithMaxBatchBytes(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxBatch = n
		}
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

// Sender posts envelopes to one collector. It implements pipeline.Sender.
type Sender struct {
	client     *httpdrain.Client
	index      string
	source     string
	sourceType string
	maxBatch   int
	logger     *wlog.Logger
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
		url:        os.Getenv("SPLUNK_HEC_URL"),
		token:      os.Getenv("SPLUNK_HEC_TOKEN"),
		index:      os.Getenv("SPLUNK_INDEX"),
		source:     defaultSource,
		sourceType: os.Getenv("SPLUNK_SOURCETYPE"),
		maxBatch:   defaultMaxBatch,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.url == "" {
		return nil, nil, fmt.Errorf("splunk: SPLUNK_HEC_URL is required")
	}
	if c.token == "" {
		return nil, nil, fmt.Errorf("splunk: SPLUNK_HEC_TOKEN is required")
	}
	if c.sourceType == "" {
		c.sourceType = defaultSourceType
	}
	clientOpts := []httpdrain.Option{
		httpdrain.WithSource("splunk"),
		httpdrain.WithHeader("Authorization", "Splunk "+c.token),
		httpdrain.WithHeader("X-Splunk-Request-Channel", newChannel()),
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
		client:     httpdrain.New(strings.TrimRight(c.url, "/")+"/services/collector/event", clientOpts...),
		index:      c.index,
		source:     c.source,
		sourceType: c.sourceType,
		maxBatch:   c.maxBatch,
	}, c.pipelineOpts, nil
}

// Setup keeps the Logger, so a capacity warning can report through it.
func (s *Sender) Setup(l *wlog.Logger) error {
	s.logger = l
	return nil
}

// SendBatch posts the events, split at the byte cap, and maps the HEC code.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	var retry, dropped []int
	reason := ""
	for start := 0; start < len(events); {
		end := s.chunkEnd(events, start)
		indexes := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			indexes = append(indexes, i)
		}
		r, d, why, err := s.sendChunk(ctx, events[start:end], indexes, true)
		if err != nil {
			return err
		}
		retry = append(retry, r...)
		dropped = append(dropped, d...)
		if why != "" {
			reason = why
		}
		start = end
	}
	if len(retry) == 0 && len(dropped) == 0 {
		return nil
	}
	return &pipeline.PartialError{Retry: retry, Dropped: dropped, Reason: reason}
}

// sendChunk posts one chunk and maps its code. With split on, code 6 splits the chunk in
// half and sends each half once.
func (s *Sender) sendChunk(ctx context.Context, events []map[string]any, indexes []int, split bool) (retry, dropped []int, reason string, err error) {
	body, err := envelopeBody(events, s.index, s.source, s.sourceType)
	if err != nil {
		return nil, nil, "", fmt.Errorf("splunk: build body: %w", err)
	}
	answer, err := s.client.PostFor(ctx, body, "application/json")
	if err != nil {
		return nil, nil, "", err
	}
	code := codeOf(answer)
	switch {
	case code == 0:
		return nil, nil, "", nil
	case code == 24 || code == 25:
		s.reportBackpressure(code)
		return nil, nil, "", nil
	case code == 6:
		if !split || len(events) <= 1 {
			return nil, indexes, "hec_code_6", nil
		}
		half := len(events) / 2
		r1, d1, why1, err := s.sendChunk(ctx, events[:half], indexes[:half], false)
		if err != nil {
			return nil, nil, "", err
		}
		r2, d2, why2, err := s.sendChunk(ctx, events[half:], indexes[half:], false)
		if err != nil {
			return nil, nil, "", err
		}
		return append(r1, r2...), append(d1, d2...), firstReason(why1, why2), nil
	case retryCodes[code]:
		return nil, nil, "", &hecError{code: code}
	default:
		return nil, indexes, "hec_code_" + strconv.Itoa(code), nil
	}
}

// chunkEnd returns the end index of the next chunk, so the body stays under the byte cap.
func (s *Sender) chunkEnd(events []map[string]any, start int) int {
	size := 0
	for i := start; i < len(events); i++ {
		body, err := envelopeBody(events[i:i+1], s.index, s.source, s.sourceType)
		if err != nil {
			return i + 1
		}
		if i > start && size+len(body) > s.maxBatch {
			return i
		}
		size += len(body)
	}
	return len(events)
}

// envelope is one Splunk event.
type envelope struct {
	Time       json.Number       `json:"time"`
	Host       string            `json:"host,omitempty"`
	Source     string            `json:"source,omitempty"`
	SourceType string            `json:"sourcetype,omitempty"`
	Index      string            `json:"index,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
	Event      map[string]any    `json:"event"`
}

// envelopeBody builds one body: one envelope per event, joined with newlines.
func envelopeBody(events []map[string]any, index, source, sourcetype string) ([]byte, error) {
	var buf strings.Builder
	for _, event := range events {
		line, err := json.Marshal(envelopeOf(event, index, source, sourcetype))
		if err != nil {
			return nil, err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return []byte(buf.String()), nil
}

// envelopeOf builds one envelope.
func envelopeOf(event map[string]any, index, source, sourcetype string) envelope {
	return envelope{
		Time:       epochSeconds(event),
		Host:       serviceField(event, "instance"),
		Source:     source,
		SourceType: sourcetype,
		Index:      index,
		Fields:     lowCardinalityFields(event),
		Event:      event,
	}
}

// lowCardinalityFields returns the flat strings Splunk indexes as fields.
func lowCardinalityFields(event map[string]any) map[string]string {
	out := map[string]string{}
	for _, pair := range []struct{ path, name string }{
		{"level", "level"},
		{"kind", "kind"},
		{"outcome", "outcome"},
		{"service.name", "service"},
		{"service.env", "env"},
	} {
		if value, ok := pathValue(event, pair.path); ok {
			if text, ok := value.(string); ok && text != "" {
				out[pair.name] = text
			}
		}
	}
	return out
}

// serviceField reads one field of the service group.
func serviceField(event map[string]any, name string) string {
	value, _ := pathValue(event, "service."+name)
	text, _ := value.(string)
	return text
}

// epochSeconds reads the event timestamp as epoch seconds with three decimals.
func epochSeconds(event map[string]any) json.Number {
	text, _ := event["timestamp"].(string)
	stamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		stamp = time.Now()
	}
	return json.Number(strconv.FormatFloat(float64(stamp.UnixMilli())/1000, 'f', 3, 64))
}

// hecResponse is the answer to one HEC request.
type hecResponse struct {
	Code int    `json:"code"`
	Text string `json:"text"`
}

// codeOf reads the HEC code, or a permanent code when the body is unreadable.
func codeOf(answer []byte) int {
	var response hecResponse
	if err := json.Unmarshal(answer, &response); err != nil {
		return -1
	}
	return response.Code
}

// hecError is a HEC code that the pipeline should retry.
type hecError struct {
	code int
}

// Error names the code.
func (e *hecError) Error() string { return "splunk: hec code " + strconv.Itoa(e.code) }

// Retryable reports that the code is worth another try.
func (e *hecError) Retryable() bool { return true }

// RetryAfter is zero, because a HEC code carries no server wait.
func (e *hecError) RetryAfter() time.Duration { return 0 }

// reportBackpressure tells the caller that Splunk is near its capacity.
func (s *Sender) reportBackpressure(code int) {
	if s.logger == nil {
		return
	}
	s.logger.Report(wlog.Problem{
		Code:    "WLOG_DRAIN_BACKPRESSURE",
		Source:  "splunk",
		Message: "the collector is near its capacity, hec code " + strconv.Itoa(code),
	})
}

// firstReason returns the first non-empty reason.
func firstReason(reasons ...string) string {
	for _, reason := range reasons {
		if reason != "" {
			return reason
		}
	}
	return ""
}

// pathValue reads a dotted path from an event.
func pathValue(event map[string]any, path string) (any, bool) {
	var current any = event
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// newChannel returns one random channel id for the life of the drain.
func newChannel() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
