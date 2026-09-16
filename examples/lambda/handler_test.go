package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/lambdacontext"

	"github.com/jeremygprawira/wlog"
)

// recorder collects events and counts Close calls.
type recorder struct {
	mu     sync.Mutex
	events []map[string]any
	closes int
}

func (r *recorder) Send(_ context.Context, event map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recorder) Close(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
	return nil
}

// lambdaCtx builds a context the way the Lambda runtime does.
func lambdaCtx(t *testing.T) context.Context {
	t.Helper()
	base := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID:       "req-1",
		InvokedFunctionArn: "arn:aws:lambda:us-east-1:1:function:checkout",
	})
	ctx, cancel := context.WithDeadline(base, time.Now().Add(5*time.Second))
	t.Cleanup(cancel)
	return ctx
}

// TestHandler_SetsFaasFieldsAndFlushes proves one invocation emits one event with the
// faas fields, and the drains are flushed before the handler returns.
func TestHandler_SetsFaasFieldsAndFlushes(t *testing.T) {
	rec := &recorder{}
	log := wlog.New(wlog.WithDrains(rec), wlog.WithFormat(wlog.FormatJSON))
	handler := Handler(log, func(context.Context, map[string]any) (any, error) { return "ok", nil })

	if _, err := handler(lambdaCtx(t), map[string]any{"a": 1}); err != nil {
		t.Fatalf("handler: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 1 {
		t.Fatalf("events = %d, want 1", len(rec.events))
	}
	event := rec.events[0]
	if event["faas.request_id"] != "req-1" {
		t.Errorf("faas.request_id = %v, want req-1", event["faas.request_id"])
	}
	if event["faas.name"] != "checkout" {
		t.Errorf("faas.name = %v, want checkout", event["faas.name"])
	}
	if event["faas.cold_start"] != true {
		t.Errorf("faas.cold_start = %v, want true on the first call", event["faas.cold_start"])
	}
	if event["faas.remaining_ms"] == nil {
		t.Error("faas.remaining_ms missing")
	}
	if rec.closes == 0 {
		t.Error("Close was not called, so a frozen Lambda would lose the batch")
	}
}

// TestHandler_SecondCallIsWarm proves the closure remembers the first invocation.
func TestHandler_SecondCallIsWarm(t *testing.T) {
	rec := &recorder{}
	log := wlog.New(wlog.WithDrains(rec), wlog.WithFormat(wlog.FormatJSON))
	handler := Handler(log, func(context.Context, map[string]any) (any, error) { return "ok", nil })

	ctx := lambdaCtx(t)
	if _, err := handler(ctx, nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := handler(ctx, nil); err != nil {
		t.Fatalf("second call: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.events[1]["faas.cold_start"] != false {
		t.Errorf("second call cold_start = %v, want false", rec.events[1]["faas.cold_start"])
	}
}
