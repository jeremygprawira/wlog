// This file drives a real asynq server over miniredis, so one processed task proves the
// fields of the table and the trace of the enqueue headers end to end.
package wlogasynq

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"

	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAsynq_C1_ServerRecordsTask proves that a server which processes one task records its
// id, its queue, its attempt, its max attempts, and the trace of the enqueue headers.
func TestAsynq_C1_ServerRecordsTask(t *testing.T) {
	mr := miniredis.RunT(t)
	redis := asynq.RedisClientOpt{Addr: mr.Addr()}
	log, rec := wlogtest.New(t)

	client := asynq.NewClient(redis)
	t.Cleanup(func() { _ = client.Close() })

	handled := make(chan struct{})
	mux := asynq.NewServeMux()
	mux.Use(Middleware(log))
	mux.HandleFunc("reindex:orders", func(context.Context, *asynq.Task) error {
		close(handled)
		return nil
	})
	srv := asynq.NewServer(redis, asynq.Config{Concurrency: 1, Queues: map[string]int{"default": 1}})
	if err := srv.Start(mux); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Shutdown)

	ctx, end := tracedContext(t, log)
	info, err := Enqueue(ctx, client, "reindex:orders", []byte("payload"), asynq.Queue("default"), asynq.MaxRetry(3))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	end()

	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not process the task")
	}

	got := waitForEvent(t, rec, "job")
	job, _ := got["job"].(map[string]any)
	for key, want := range map[string]any{
		"system": "asynq", "name": "reindex:orders", "id": info.ID,
		"queue": "default", "attempt": 1, "max_attempts": 4,
	} {
		if !conformance.Equal(job[key], want) {
			t.Errorf("job.%s = %v, want %v", key, job[key], want)
		}
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the enqueue header", trace["trace_id"])
	}
	record := firstCall(t, eventOfKind(t, rec, "work"))
	if trace["parent_span_id"] != record["span_id"] {
		t.Errorf("trace.parent_span_id = %v, want the span id of the enqueue call %v", trace["parent_span_id"], record["span_id"])
	}
}

// waitForEvent polls the recorder until the event of one kind lands, because the server
// emits it on its own goroutine after the handler returns.
func waitForEvent(t *testing.T, rec *wlogtest.Recorder, kind string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range rec.Events() {
			if event["kind"] == kind {
				return event
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s event recorded in %d events", kind, len(rec.Events()))
	return nil
}

// TestAsynq_C1_ExhaustedRetryRecordsDiscard proves that a task on its last attempt records
// result discard, which is the state asynq keeps for it.
func TestAsynq_C1_ExhaustedRetryRecordsDiscard(t *testing.T) {
	mr := miniredis.RunT(t)
	redis := asynq.RedisClientOpt{Addr: mr.Addr()}
	log, rec := wlogtest.New(t)

	client := asynq.NewClient(redis)
	t.Cleanup(func() { _ = client.Close() })

	handled := make(chan struct{})
	mux := asynq.NewServeMux()
	mux.Use(Middleware(log))
	mux.HandleFunc("reindex", func(context.Context, *asynq.Task) error {
		close(handled)
		return errString("boom")
	})
	srv := asynq.NewServer(redis, asynq.Config{Concurrency: 1, Queues: map[string]int{"default": 1}})
	if err := srv.Start(mux); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Shutdown)

	ctx, end := tracedContext(t, log)
	if _, err := Enqueue(ctx, client, "reindex", nil, asynq.Queue("default"), asynq.MaxRetry(0)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	end()

	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not process the task")
	}

	got := waitForEvent(t, rec, "job")
	if result := jobField(t, got, "result"); result != "discard" {
		t.Errorf("job.result = %v, want discard on the last attempt", result)
	}
}

// eventOfKind returns the event of one kind, and stops the test when the run recorded none.
func eventOfKind(t *testing.T, rec *wlogtest.Recorder, kind string) map[string]any {
	t.Helper()
	for _, event := range rec.Events() {
		if event["kind"] == kind {
			return event
		}
	}
	t.Fatalf("no %s event recorded in %d events", kind, len(rec.Events()))
	return nil
}
