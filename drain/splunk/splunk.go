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
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/share"
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

	// mu guards backpressureAt, the time of the last WLOG_DRAIN_BACKPRESSURE report.
	mu             sync.Mutex
	backpressureAt time.Time
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
		gzip:       true,
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
// Each event is encoded once. The chunk size and the request body share that encoding.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	lines := make([][]byte, len(events))
	var buildErr error
	encode := func(i int) int {
		if buildErr != nil {
			return -1
		}
		if lines[i] != nil {
			return len(lines[i])
		}
		line, err := json.Marshal(envelopeOf(events[i], s.index, s.source, s.sourceType))
		if err != nil {
			buildErr = err
			return -1
		}
		lines[i] = append(line, '\n')
		return len(lines[i])
	}
	var retry, dropped []int
	reason := ""
	for start := 0; start < len(events); {
		end := share.ChunkEnd(len(events), start, s.maxBatch, encode)
		if buildErr != nil {
			return fmt.Errorf("splunk: build body: %w", buildErr)
		}
		indexes := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			indexes = append(indexes, i)
		}
		r, d, why, err := s.sendChunk(ctx, lines[start:end], events[start:end], indexes, true)
		if err != nil {
			// The chunks that landed must not be sent again: a retryable fault retries
			// this chunk and the rest, and reports the drops so far.
			var re interface{ Retryable() bool }
			if !errors.As(err, &re) || !re.Retryable() {
				return err
			}
			for i := start; i < len(events); i++ {
				retry = append(retry, i)
			}
			if reason == "" {
				reason = statusReason(err)
			}
			return &pipeline.PartialError{Retry: retry, Dropped: dropped, Reason: reason}
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
func (s *Sender) sendChunk(ctx context.Context, lines [][]byte, events []map[string]any, indexes []int, split bool) (retry, dropped []int, reason string, err error) {
	body := joinLines(lines)
	answer, err := s.client.PostFor(ctx, body, "application/json")
	if err != nil {
		// Splunk sends the HEC code with a 4xx or a 503 answer, and the body holds it,
		// so a refused request is read like an accepted one.
		var statusErr *httpdrain.StatusError
		if !errors.As(err, &statusErr) {
			return nil, nil, "", err
		}
		// A 413 says the request is too large: the chunk is halved, and each half is sent
		// once. A single event that still gets a 413 is dropped.
		if statusErr.Status == http.StatusRequestEntityTooLarge {
			if len(events) <= 1 {
				return nil, indexes, "too_large", nil
			}
			half := len(events) / 2
			r1, d1, why1, err := s.sendChunk(ctx, lines[:half], events[:half], indexes[:half], split)
			if err != nil {
				return nil, nil, "", err
			}
			r2, d2, why2, err := s.sendChunk(ctx, lines[half:], events[half:], indexes[half:], split)
			if err != nil {
				return nil, nil, "", err
			}
			return append(r1, r2...), append(d1, d2...), firstReason(why1, why2), nil
		}
		if len(statusErr.Body) == 0 {
			return nil, nil, "", err
		}
		answer = statusErr.Body
	}
	code := codeOf(answer)
	if code < 0 {
		// The answer held no readable code, so the request failed in a way that says
		// nothing about the events. A retry is right; dropping them is not.
		if err != nil {
			return nil, nil, "", err
		}
		return nil, nil, "", fmt.Errorf("splunk: the answer holds no HEC code")
	}
	if code == 0 {
		// A failed request whose answer holds no code keeps its own error.
		if err != nil {
			return nil, nil, "", err
		}
		return nil, nil, "", nil
	}
	switch {
	case code == 24 || code == 25:
		s.reportBackpressure(code)
		return nil, nil, "", nil
	case code == 6:
		// Code 6 says one event of the chunk is malformed, so the chunk is halved until
		// one event is left. Each half is sent once, as shared rule 4 says.
		if !split || len(events) <= 1 {
			return nil, indexes, "hec_code_6", nil
		}
		half := len(events) / 2
		r1, d1, why1, err := s.sendChunk(ctx, lines[:half], events[:half], indexes[:half], true)
		if err != nil {
			return nil, nil, "", err
		}
		r2, d2, why2, err := s.sendChunk(ctx, lines[half:], events[half:], indexes[half:], true)
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

// statusReason names a failed chunk for the PartialError. It never holds a response body.
func statusReason(err error) string {
	var statusErr *httpdrain.StatusError
	if errors.As(err, &statusErr) {
		return "status_" + strconv.Itoa(statusErr.Status)
	}
	var hec *hecError
	if errors.As(err, &hec) {
		return "hec_code_" + strconv.Itoa(hec.code)
	}
	return "transport"
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

// joinLines returns the encoded lines as one request body.
func joinLines(lines [][]byte) []byte {
	size := 0
	for _, line := range lines {
		size += len(line)
	}
	body := make([]byte, 0, size)
	for _, line := range lines {
		body = append(body, line...)
	}
	return body
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
		if value, ok := share.Path(event, pair.path); ok {
			if text, ok := value.(string); ok && text != "" {
				out[pair.name] = text
			}
		}
	}
	return out
}

// serviceField reads one field of the service group.
func serviceField(event map[string]any, name string) string {
	value, _ := share.Path(event, "service."+name)
	text, _ := value.(string)
	return text
}

// epochSeconds reads the event timestamp as epoch seconds with three decimals.
func epochSeconds(event map[string]any) json.Number {
	text, _ := event["timestamp"].(string)
	stamp, ok := pipeline.ParseTimestamp(text)
	if !ok {
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

// reportBackpressure tells the caller that the collector is near its capacity, at most once
// per minute, so a hot batch loop cannot flood the console.
func (s *Sender) reportBackpressure(code int) {
	if s.logger == nil {
		return
	}
	s.mu.Lock()
	now := time.Now()
	if !s.backpressureAt.IsZero() && now.Sub(s.backpressureAt) < time.Minute {
		s.mu.Unlock()
		return
	}
	s.backpressureAt = now
	s.mu.Unlock()
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

// newChannel returns one random channel id for the life of the drain.
func newChannel() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
