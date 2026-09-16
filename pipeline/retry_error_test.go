package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/pipeline"
)

// permanentError implements pipeline.RetryError to say "don't bother retrying".
type permanentError struct{}

func (permanentError) Error() string             { return "permanent" }
func (permanentError) Retryable() bool           { return false }
func (permanentError) RetryAfter() time.Duration { return 0 }

type permanentSender struct{ calls int }

func (s *permanentSender) SendBatch(context.Context, []map[string]any) error {
	s.calls++
	return permanentError{}
}

func TestPipeline_RetryError_PermanentStopsImmediately(t *testing.T) {
	sender := &permanentSender{}
	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(1), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(5), pipeline.InitialDelay(2*time.Millisecond),
		pipeline.OnDropped(drop.record),
	)
	defer func() { _ = drain.(interface{ Close(context.Context) error }).Close(context.Background()) }()

	drain.Send(context.Background(), mkEvent(1))

	waitFor(t, time.Second, func() bool { return drop.count() > 0 })
	time.Sleep(20 * time.Millisecond) // make sure no further attempts trickle in
	if sender.calls != 1 {
		t.Errorf("SendBatch called %d times, want 1 (a permanent error should not retry)", sender.calls)
	}
}
