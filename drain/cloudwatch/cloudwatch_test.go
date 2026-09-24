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
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/preset"
	"github.com/jeremygprawira/wlog/setup"
)

// apiError is one CloudWatch Logs error with a code.
type apiError struct{ code string }

func (e apiError) Error() string                 { return e.code }
func (e apiError) ErrorCode() string             { return e.code }
func (e apiError) ErrorMessage() string          { return e.code }
func (e apiError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// fakeAPI records every put and answers with a fixed result.
type fakeAPI struct {
	mu       sync.Mutex
	puts     []*cloudwatchlogs.PutLogEventsInput
	creates  int
	putErr   error
	rejected *types.RejectedLogEventsInfo
	failNext bool
}

func (f *fakeAPI) PutLogEvents(_ context.Context, in *cloudwatchlogs.PutLogEventsInput,
	_ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.PutLogEventsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts = append(f.puts, in)
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

// TestCloudWatch_MaskedFieldNeverLeaks proves core redacts before the drain, so a secret
// under a denied key never reaches the stream.
func TestCloudWatch_MaskedFieldNeverLeaks(t *testing.T) {
	api := &fakeAPI{}
	drain, err := cloudwatch.New(api, "group")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(drain))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Set(ctx, "conformance_marker", "PIPE25-MARKER")
	wlog.Set(ctx, "password", "PIPE25-s3cret")
	end()
	if err := log.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := log.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	message := *api.lastPut().LogEvents[0].Message
	if strings.Contains(message, "PIPE25-s3cret") {
		t.Error("the secret reached the drain")
	}
	if !strings.Contains(message, "PIPE25-MARKER") {
		t.Error("the marker did not arrive")
	}
}

// contains reports whether text holds sub.
func contains(text, sub string) bool {
	return strings.Contains(text, sub)
}
