// Package cloudwatch sends wlog events to a CloudWatch Logs stream. The app builds the
// client, so credentials, region, and SDK retries stay with the app:
//
//	client := cloudwatchlogs.NewFromConfig(cfg)
//	drain, err := cloudwatch.New(client, "my-group", cloudwatch.WithStream("my-stream"))
//
// The drain sorts each batch by timestamp, splits it at 10,000 events, 1,048,576 bytes,
// and a 24 hour span, and maps the rejected ranges back to the batch. It never creates a
// log group.
package cloudwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/smithy-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/setup"
)

// Limits AWS documents.
const (
	maxEvents        = 10000
	maxBytes         = 1048576
	perEventOverhead = 26
	maxSpan          = 24 * time.Hour
)

// retryableCodes are the API error codes worth another try.
var retryableCodes = map[string]bool{
	"ThrottlingException":         true,
	"ServiceUnavailableException": true,
	"InternalFailure":             true,
	"RequestTimeoutException":     true,
	"ExpiredTokenException":       true,
}

// API is the part of the CloudWatch Logs client the drain needs.
type API interface {
	PutLogEvents(ctx context.Context, in *cloudwatchlogs.PutLogEventsInput,
		opts ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.PutLogEventsOutput, error)
	CreateLogStream(ctx context.Context, in *cloudwatchlogs.CreateLogStreamInput,
		opts ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.CreateLogStreamOutput, error)
}

// config holds the resolved options.
type config struct {
	stream       string
	preset       wlog.OutputPreset
	createStream bool
	pipelineOpts []pipeline.Option
}

// Option configures New.
type Option func(*config)

// WithStream sets the log stream. The default is the machine host name.
func WithStream(name string) Option {
	return func(c *config) {
		if name != "" {
			c.stream = name
		}
	}
}

// WithPreset writes each message with an output preset, such as preset.EMF(). Without
// one, the message is the canonical event JSON.
func WithPreset(p wlog.OutputPreset) Option {
	return func(c *config) {
		if p != nil {
			c.preset = p
		}
	}
}

// WithCreateStream creates the stream once on ResourceNotFoundException. The default is
// true.
func WithCreateStream(on bool) Option { return func(c *config) { c.createStream = on } }

// WithPipeline sets the pipeline options New wraps the sender with.
func WithPipeline(opts ...pipeline.Option) Option {
	return func(c *config) { c.pipelineOpts = append(c.pipelineOpts, opts...) }
}

// Sender puts batches to one stream. It implements pipeline.Sender.
type Sender struct {
	client       API
	group        string
	stream       string
	preset       wlog.OutputPreset
	createStream bool

	mu      sync.Mutex
	created bool
}

// New returns the drain with the pipeline defaults, or with the options WithPipeline set.
func New(client API, group string, opts ...Option) (wlog.Drain, error) {
	s, popts, err := newSender(client, group, opts...)
	if err != nil {
		return nil, err
	}
	return pipeline.Wrap(s, popts...), nil
}

// NewSender returns the raw sender, for a caller that builds its own pipeline.
func NewSender(client API, group string, opts ...Option) (*Sender, error) {
	s, _, err := newSender(client, group, opts...)
	return s, err
}

// MustNew is New, but panics on a configuration error. Use it in main.
func MustNew(client API, group string, opts ...Option) wlog.Drain {
	d, err := New(client, group, opts...)
	if err != nil {
		panic(err)
	}
	return d
}

// newSender resolves one configuration.
func newSender(client API, group string, opts ...Option) (*Sender, []pipeline.Option, error) {
	if client == nil {
		return nil, nil, errors.New("cloudwatch: client is required")
	}
	if group == "" {
		return nil, nil, errors.New("cloudwatch: group is required")
	}
	c := config{createStream: true}
	for _, opt := range opts {
		opt(&c)
	}
	if c.stream == "" {
		c.stream = os.Getenv("WLOG_CLOUDWATCH_STREAM")
	}
	if c.stream == "" {
		if host, err := os.Hostname(); err == nil && host != "" {
			c.stream = host
		}
	}
	if c.stream == "" {
		c.stream = "wlog"
	}
	return &Sender{
		client:       client,
		group:        group,
		stream:       c.stream,
		preset:       c.preset,
		createStream: c.createStream,
	}, c.pipelineOpts, nil
}

// entry is one event with its place in the batch.
type entry struct {
	index     int
	timestamp int64
	message   string
}

// SendBatch sorts the events, splits them, and puts each chunk.
func (s *Sender) SendBatch(ctx context.Context, events []map[string]any) error {
	entries := make([]entry, 0, len(events))
	for i, event := range events {
		message, err := s.messageOf(event)
		if err != nil {
			return fmt.Errorf("cloudwatch: build message: %w", err)
		}
		entries = append(entries, entry{index: i, timestamp: startMillis(event), message: message})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].timestamp < entries[j].timestamp })

	var dropped []int
	reason := ""
	for _, chunk := range chunks(entries) {
		chunkDropped, chunkReason, err := s.putChunk(ctx, chunk)
		if err != nil {
			return err
		}
		dropped = append(dropped, chunkDropped...)
		if chunkReason != "" {
			reason = chunkReason
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	return &pipeline.PartialError{Dropped: dropped, Reason: reason}
}

// chunks splits the sorted entries at the count, the byte cap, and the span.
func chunks(entries []entry) [][]entry {
	var out [][]entry
	var current []entry
	bytes := 0
	for _, e := range entries {
		size := len(e.message) + perEventOverhead
		if len(current) > 0 && (len(current) >= maxEvents ||
			bytes+size > maxBytes ||
			e.timestamp-current[0].timestamp >= maxSpan.Milliseconds()) {
			out = append(out, current)
			current = nil
			bytes = 0
		}
		current = append(current, e)
		bytes += size
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

// putChunk puts one chunk and maps the rejected ranges.
func (s *Sender) putChunk(ctx context.Context, chunk []entry) (dropped []int, reason string, err error) {
	logEvents := make([]types.InputLogEvent, 0, len(chunk))
	for _, e := range chunk {
		logEvents = append(logEvents, types.InputLogEvent{
			Message:   aws.String(e.message),
			Timestamp: aws.Int64(e.timestamp),
		})
	}
	out, err := s.client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String(s.group),
		LogStreamName: aws.String(s.stream),
		LogEvents:     logEvents,
	})
	if err != nil {
		return nil, "", s.mapError(ctx, err)
	}
	if out == nil || out.RejectedLogEventsInfo == nil {
		return nil, "", nil
	}
	info := out.RejectedLogEventsInfo
	if info.TooNewLogEventStartIndex != nil {
		for i := clamp(int(*info.TooNewLogEventStartIndex), len(chunk)); i < len(chunk); i++ {
			dropped = append(dropped, chunk[i].index)
		}
		reason = "too_new"
	}
	if info.TooOldLogEventEndIndex != nil {
		for i := 0; i < clamp(int(*info.TooOldLogEventEndIndex), len(chunk)); i++ {
			dropped = append(dropped, chunk[i].index)
		}
		reason = "too_old"
	}
	if info.ExpiredLogEventEndIndex != nil {
		for i := 0; i < clamp(int(*info.ExpiredLogEventEndIndex), len(chunk)); i++ {
			dropped = append(dropped, chunk[i].index)
		}
		reason = "expired"
	}
	return dropped, reason, nil
}

// clamp keeps an index inside the chunk.
func clamp(index, size int) int {
	if index < 0 {
		return 0
	}
	if index > size {
		return size
	}
	return index
}

// mapError turns one API error into a retryable or a permanent error. It creates the
// stream once on ResourceNotFoundException.
func (s *Sender) mapError(ctx context.Context, err error) error {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	code := apiErr.ErrorCode()
	if code == "ResourceNotFoundException" {
		if s.createStream {
			if createErr := s.createOnce(ctx); createErr != nil {
				return createErr
			}
		}
		// The stream now exists, so the next attempt can put the batch.
		return fmt.Errorf("cloudwatch: stream created, retry: %w", err)
	}
	if retryableCodes[code] {
		return fmt.Errorf("cloudwatch: %s: %w", code, err)
	}
	return &codeError{code: code}
}

// createOnce creates the stream at most once.
func (s *Sender) createOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.created {
		return nil
	}
	s.created = true
	_, err := s.client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(s.group),
		LogStreamName: aws.String(s.stream),
	})
	if err != nil {
		return fmt.Errorf("cloudwatch: create stream: %w", err)
	}
	return nil
}

// codeError is a permanent API error, so the pipeline drops the batch rather than
// retrying it verbatim.
type codeError struct{ code string }

// Error names the code.
func (e *codeError) Error() string { return "cloudwatch: " + e.code }

// Retryable reports that the code is permanent.
func (e *codeError) Retryable() bool { return false }

// RetryAfter is zero, because a permanent error gets no wait.
func (e *codeError) RetryAfter() time.Duration { return 0 }

// messageOf returns the message of one event: the preset output, or the canonical event
// JSON.
func (s *Sender) messageOf(event map[string]any) (string, error) {
	if s.preset == nil {
		body, err := json.Marshal(event)
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
	body, err := json.Marshal(s.preset.Apply(event))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// startMillis reads the event start in epoch milliseconds.
func startMillis(event map[string]any) int64 {
	text, _ := event["timestamp"].(string)
	stamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Now().UnixMilli()
	}
	return stamp.UnixMilli()
}

// Factory returns the setup factory for a drain that uses the app's own client.
func Factory(client API) setup.Factory {
	return setup.Factory{
		Name: "cloudwatch",
		Vars: []setup.Var{
			{Name: "WLOG_CLOUDWATCH_GROUP", Required: true},
			{Name: "WLOG_CLOUDWATCH_STREAM"},
		},
		New: func(env setup.Env) (wlog.Drain, error) {
			group, _ := env.Lookup("WLOG_CLOUDWATCH_GROUP")
			var opts []Option
			if stream, ok := env.Lookup("WLOG_CLOUDWATCH_STREAM"); ok && stream != "" {
				opts = append(opts, WithStream(stream))
			}
			return New(client, group, opts...)
		},
	}
}
