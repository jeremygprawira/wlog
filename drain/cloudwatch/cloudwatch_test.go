package cloudwatch_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/smithy-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/cloudwatch"
	"github.com/jeremygprawira/wlog/internal/conformance"
	drainconformance "github.com/jeremygprawira/wlog/internal/conformance/drain"
	"github.com/jeremygprawira/wlog/internal/httpfake"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/preset"
	"github.com/jeremygprawira/wlog/setup"
)

// apiError is one CloudWatch Logs error with a code and a message.
type apiError struct {
	code string
	msg  string
}

func (e apiError) Error() string                 { return e.code + ": " + e.message() }
func (e apiError) ErrorCode() string             { return e.code }
func (e apiError) ErrorMessage() string          { return e.message() }
func (e apiError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// message returns the AWS text, which stands in for a response body.
func (e apiError) message() string {
	if e.msg != "" {
		return e.msg
	}
	return e.code
}

// fakeAPI records every put and answers with a fixed result.
type fakeAPI struct {
	mu        sync.Mutex
	puts      []*cloudwatchlogs.PutLogEventsInput
	creates   int
	putErr    error
	createErr error
	// failCreateNext fails the next CreateLogStream call with createErr.
	failCreateNext bool
	rejected       *types.RejectedLogEventsInfo
	failNext       bool
	// failAt is the 1-based put number that fails with putErr, or 0 for none.
	failAt int
}

func (f *fakeAPI) PutLogEvents(_ context.Context, in *cloudwatchlogs.PutLogEventsInput,
	_ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.PutLogEventsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts = append(f.puts, in)
	if f.failAt > 0 {
		if len(f.puts) == f.failAt {
			err := f.putErr
			f.putErr = nil
			return nil, err
		}
		return &cloudwatchlogs.PutLogEventsOutput{RejectedLogEventsInfo: f.rejected}, nil
	}
	if f.failNext {
		f.failNext = false
		err := f.putErr
		f.putErr = nil
		return nil, err
	}
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &cloudwatchlogs.PutLogEventsOutput{RejectedLogEventsInfo: f.rejected}, nil
}

func (f *fakeAPI) CreateLogStream(_ context.Context, _ *cloudwatchlogs.CreateLogStreamInput,
	_ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.CreateLogStreamOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if f.failCreateNext {
		f.failCreateNext = false
		return nil, f.createErr
	}
	return &cloudwatchlogs.CreateLogStreamOutput{}, nil
}

// lastPut returns the most recent put input.
func (f *fakeAPI) lastPut() *cloudwatchlogs.PutLogEventsInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.puts) == 0 {
		return nil
	}
	return f.puts[len(f.puts)-1]
}

// putCount returns how many puts arrived.
func (f *fakeAPI) putCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.puts)
}

// event returns one event at a timestamp.
func event(stamp string) map[string]any {
	return map[string]any{
		"timestamp": stamp,
		"level":     "info",
		"kind":      "job",
		"outcome":   "success",
		"summary":   "cleanup",
	}
}

// TestCloudWatch_SortAndSplit proves an unsorted batch arrives sorted.
func TestCloudWatch_SortAndSplit(t *testing.T) {
	api := &fakeAPI{}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{
		event("2026-09-22T12:00:00Z"),
		event("2026-09-22T10:00:00Z"),
		event("2026-09-22T11:00:00Z"),
	})
	if err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	put := api.lastPut()
	if len(put.LogEvents) != 3 {
		t.Fatalf("events = %d, want 3", len(put.LogEvents))
	}
	for i := 1; i < len(put.LogEvents); i++ {
		if *put.LogEvents[i].Timestamp <= *put.LogEvents[i-1].Timestamp {
			t.Errorf("event %d is not after event %d", i, i-1)
		}
	}
}

// TestCloudWatch_SplitBySpan proves a batch that spans 25 hours becomes two calls.
func TestCloudWatch_SplitBySpan(t *testing.T) {
	api := &fakeAPI{}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	err = sender.SendBatch(context.Background(), []map[string]any{
		event(start.Format(time.RFC3339Nano)),
		event(start.Add(25 * time.Hour).Format(time.RFC3339Nano)),
	})
	if err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if api.putCount() != 2 {
		t.Errorf("puts = %d, want 2", api.putCount())
	}
}

// TestCloudWatch_RejectedRanges proves the rejected ranges map to the original indexes.
func TestCloudWatch_RejectedRanges(t *testing.T) {
	api := &fakeAPI{rejected: &types.RejectedLogEventsInfo{TooOldLogEventEndIndex: aws.Int32(1)}}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{
		event("2026-09-22T10:00:00Z"),
		event("2026-09-22T10:00:01Z"),
		event("2026-09-22T10:00:02Z"),
	})
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if len(partial.Dropped) != 1 || partial.Dropped[0] != 0 {
		t.Errorf("Dropped = %v, want [0]", partial.Dropped)
	}
	if partial.Reason != "too_old" {
		t.Errorf("Reason = %q, want too_old", partial.Reason)
	}

	newer := &fakeAPI{rejected: &types.RejectedLogEventsInfo{TooNewLogEventStartIndex: aws.Int32(1)}}
	newerSender, err := cloudwatch.NewSender(newer, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = newerSender.SendBatch(context.Background(), []map[string]any{
		event("2026-09-22T10:00:00Z"),
		event("2026-09-22T10:00:01Z"),
		event("2026-09-22T10:00:02Z"),
	})
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if len(partial.Dropped) != 2 || partial.Dropped[0] != 1 || partial.Dropped[1] != 2 {
		t.Errorf("Dropped = %v, want [1 2]", partial.Dropped)
	}
	if partial.Reason != "too_new" {
		t.Errorf("Reason = %q, want too_new", partial.Reason)
	}
}

// TestCloudWatch_CreateStreamOnce proves ResourceNotFoundException creates the stream
// once and then succeeds.
func TestCloudWatch_CreateStreamOnce(t *testing.T) {
	api := &fakeAPI{putErr: apiError{code: "ResourceNotFoundException"}, failNext: true}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")}); err == nil {
		t.Fatal("SendBatch did not return a retryable error")
	}
	if api.creates != 1 {
		t.Fatalf("CreateLogStream calls = %d, want 1", api.creates)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")}); err != nil {
		t.Fatalf("second SendBatch: %v", err)
	}
	if api.creates != 1 {
		t.Errorf("CreateLogStream calls = %d after success, want 1", api.creates)
	}
}

// TestCloudWatch_D3_CreatesTheStreamAfterAFailedCreate proves a later missing-stream error
// still creates the stream, so one throttled create does not stop the drain.
func TestCloudWatch_D3_CreatesTheStreamAfterAFailedCreate(t *testing.T) {
	api := &fakeAPI{
		putErr:         apiError{code: "ResourceNotFoundException"},
		createErr:      apiError{code: "ThrottlingException"},
		failCreateNext: true,
	}
	sender, err := cloudwatch.NewSender(api, "group", cloudwatch.WithCreateStream(true))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")}); err == nil {
		t.Fatal("the first batch returned no error")
	}
	// The stream is still missing, so the second batch creates it again. Before the fix
	// the flag was already set, so no later attempt created it.
	err = sender.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")})
	var re interface{ Retryable() bool }
	if !errors.As(err, &re) || !re.Retryable() {
		t.Fatalf("the second batch = %v, want a retryable error", err)
	}
	if api.creates != 2 {
		t.Errorf("CreateLogStream calls = %d, want 2", api.creates)
	}
}

// TestCloudWatch_Permanent proves a permanent code returns a non-retryable error.
func TestCloudWatch_Permanent(t *testing.T) {
	api := &fakeAPI{putErr: apiError{code: "InvalidParameterException"}}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")})
	var re interface{ Retryable() bool }
	if !errors.As(err, &re) || re.Retryable() {
		t.Fatalf("SendBatch = %v, want a non-retryable error", err)
	}
}

// TestCloudWatch_D10_ALaterFailureKeepsTheEarlierChunk proves a retryable fault in a later
// chunk retries that chunk and the rest, and never sends the earlier chunk again.
func TestCloudWatch_D10_ALaterFailureKeepsTheEarlierChunk(t *testing.T) {
	api := &fakeAPI{putErr: apiError{code: "ThrottlingException"}, failAt: 2}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	// The two events are more than one span apart, so the drain sends two chunks.
	err = sender.SendBatch(context.Background(), []map[string]any{
		event("2026-09-22T10:00:00Z"),
		event("2026-09-23T20:00:00Z"),
	})
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if got := api.putCount(); got != 2 {
		t.Errorf("puts = %d, want 2: the first chunk is sent once", got)
	}
	if len(partial.Retry) != 1 || partial.Retry[0] != 1 {
		t.Errorf("Retry = %v, want the second event", partial.Retry)
	}
	if partial.Reason != "ThrottlingException" {
		t.Errorf("Reason = %q, want ThrottlingException", partial.Reason)
	}
}

// TestCloudWatch_D19_TheErrorNamesTheCodeOnly proves the error holds the AWS code and not
// the AWS message, that a missing stream without create is permanent, and that
// ServiceUnavailable is worth another try.
func TestCloudWatch_D19_TheErrorNamesTheCodeOnly(t *testing.T) {
	api := &fakeAPI{putErr: apiError{code: "ThrottlingException", msg: "Rate exceeded for account 123456789012"}}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = sender.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")})
	var re interface{ Retryable() bool }
	if !errors.As(err, &re) || !re.Retryable() {
		t.Fatalf("SendBatch = %v, want a retryable error", err)
	}
	if !strings.Contains(err.Error(), "ThrottlingException") {
		t.Errorf("the error does not name the code: %v", err)
	}
	if strings.Contains(err.Error(), "Rate exceeded") {
		t.Errorf("the error holds the AWS message: %v", err)
	}

	// The stream is missing and this drain may not create it, so the fault is permanent.
	missing := &fakeAPI{putErr: apiError{code: "ResourceNotFoundException"}}
	plain, err := cloudwatch.NewSender(missing, "group", cloudwatch.WithCreateStream(false))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = plain.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")})
	if !errors.As(err, &re) || re.Retryable() {
		t.Fatalf("a missing stream without create = %v, want a permanent error", err)
	}

	// ServiceUnavailable is worth another try.
	unavailable := &fakeAPI{putErr: apiError{code: "ServiceUnavailable"}}
	third, err := cloudwatch.NewSender(unavailable, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	err = third.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")})
	if !errors.As(err, &re) || !re.Retryable() {
		t.Fatalf("ServiceUnavailable = %v, want a retryable error", err)
	}
}

// TestCloudWatch_D11_DropsAnEntryOverTheByteLimit proves an entry no request can carry is
// dropped with reason too_large and never sent.
func TestCloudWatch_D11_DropsAnEntryOverTheByteLimit(t *testing.T) {
	api := &fakeAPI{}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	big := event("2026-09-22T10:00:00Z")
	big["blob"] = strings.Repeat("x", 1<<20) // the 1 MiB entry limit
	err = sender.SendBatch(context.Background(), []map[string]any{big, event("2026-09-22T10:00:00Z")})
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	if len(partial.Dropped) != 1 || partial.Dropped[0] != 0 {
		t.Errorf("Dropped = %v, want the oversize event at index 0", partial.Dropped)
	}
	if partial.Reason != "too_large" {
		t.Errorf("Reason = %q, want too_large", partial.Reason)
	}
	if got := api.putCount(); got != 1 {
		t.Errorf("puts = %d, want one: the oversize entry is never sent", got)
	}
}

// TestCloudWatch_D6_RejectedRangesNameTheOriginalIndexes proves an unsorted batch maps each
// rejected range to the original index of the event, not to its sorted position.
func TestCloudWatch_D6_RejectedRangesNameTheOriginalIndexes(t *testing.T) {
	tooNew, tooOld, expired := int32(2), int32(1), int32(1)
	api := &fakeAPI{rejected: &types.RejectedLogEventsInfo{
		TooNewLogEventStartIndex: &tooNew,
		TooOldLogEventEndIndex:   &tooOld,
		ExpiredLogEventEndIndex:  &expired,
	}}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	// Sorted order is 1 (10:00), 2 (11:00), 0 (12:00), so the sorted positions 0 and 2 are
	// the original indexes 1 and 0.
	err = sender.SendBatch(context.Background(), []map[string]any{
		event("2026-09-22T12:00:00Z"),
		event("2026-09-22T10:00:00Z"),
		event("2026-09-22T11:00:00Z"),
	})
	var partial *pipeline.PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("SendBatch = %v, want a PartialError", err)
	}
	got := map[int]bool{}
	for _, i := range partial.Dropped {
		got[i] = true
	}
	if !got[0] || !got[1] || got[2] {
		t.Errorf("Dropped = %v, want the original indexes 0 and 1", partial.Dropped)
	}
}

// TestCloudWatch_D6_SplitsAtTheByteLimit proves a batch whose entries pass the byte limit
// together arrives as several puts.
func TestCloudWatch_D6_SplitsAtTheByteLimit(t *testing.T) {
	api := &fakeAPI{}
	sender, err := cloudwatch.NewSender(api, "group")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	big := strings.Repeat("x", 600<<10)
	events := []map[string]any{
		{"timestamp": "2026-09-22T10:00:00Z", "blob": big},
		{"timestamp": "2026-09-22T10:00:01Z", "blob": big},
	}
	if err := sender.SendBatch(context.Background(), events); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if got := api.putCount(); got != 2 {
		t.Errorf("puts = %d, want 2 for a batch over the byte limit", got)
	}
}

// TestCloudWatch_Preset proves WithPreset writes the preset output.
func TestCloudWatch_Preset(t *testing.T) {
	api := &fakeAPI{}
	sender, err := cloudwatch.NewSender(api, "group", cloudwatch.WithPreset(preset.Flat()))
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	if err := sender.SendBatch(context.Background(), []map[string]any{event("2026-09-22T10:00:00Z")}); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	message := *api.lastPut().LogEvents[0].Message
	if !contains(message, `"summary"`) {
		t.Errorf("message = %q, want the flat preset output", message)
	}
}

// TestCloudWatch_MissingConfig proves a missing client or group is refused.
func TestCloudWatch_MissingConfig(t *testing.T) {
	if _, err := cloudwatch.NewSender(nil, "group"); err == nil {
		t.Error("NewSender accepted a nil client")
	}
	if _, err := cloudwatch.NewSender(&fakeAPI{}, ""); err == nil {
		t.Error("NewSender accepted an empty group")
	}
}

// TestCloudWatch_Options proves every option reaches the sender and New wraps it.
func TestCloudWatch_Options(t *testing.T) {
	api := &fakeAPI{}
	drain, err := cloudwatch.New(api, "group",
		cloudwatch.WithStream("stream"),
		cloudwatch.WithCreateStream(false),
		cloudwatch.WithPipeline(pipeline.BatchSize(1)),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	drain.Send(context.Background(), event("2026-09-22T10:00:00Z"))
	flusher, ok := drain.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("the wrapped drain has no Flush")
	}
	if err := flusher.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if api.putCount() == 0 {
		t.Error("no put arrived")
	}
}

// TestCloudWatch_Factory proves the factory builds from its variables.
func TestCloudWatch_Factory(t *testing.T) {
	api := &fakeAPI{}
	factory := cloudwatch.Factory(api)
	if factory.Name != "cloudwatch" {
		t.Errorf("name = %q, want cloudwatch", factory.Name)
	}
	if _, err := factory.New(setup.MapEnv{"WLOG_CLOUDWATCH_GROUP": "group"}); err != nil {
		t.Fatalf("factory New: %v", err)
	}
	if _, err := factory.New(setup.MapEnv{"WLOG_CLOUDWATCH_GROUP": "group", "WLOG_CLOUDWATCH_STREAM": "stream"}); err != nil {
		t.Fatalf("factory New with stream: %v", err)
	}
}

// TestCloudWatch_MustNewPanics proves a missing group panics.
func TestCloudWatch_MustNewPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustNew did not panic")
		}
	}()
	cloudwatch.MustNew(&fakeAPI{}, "")
}

// TestCloudWatch_C1_DrainShipsEvents runs the shared drain suite: the event reaches the API
// in the v2 shape, and a redacted value never does.
func TestCloudWatch_C1_DrainShipsEvents(t *testing.T) {
	api := &fakeAPI{}
	drainconformance.Run(conformance.Tester{T: t}, []drainconformance.Case{{
		Name:  "cloudwatch",
		Build: func(*httpfake.Server) (wlog.Drain, error) { return cloudwatch.New(api, "group") },
		Local: func(*httpfake.Server) []byte {
			last := api.lastPut()
			if last == nil || len(last.LogEvents) == 0 {
				return nil
			}
			return []byte(*last.LogEvents[0].Message)
		},
	}})
}

// contains reports whether text holds sub.
func contains(text, sub string) bool {
	return strings.Contains(text, sub)
}
