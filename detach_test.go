package wlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog"
)

func TestCore_Detach_LinksToParent(t *testing.T) {
	log := wlog.New()
	parentCtx := log.WithContext(context.Background())
	parentCtx, parentEnd := wlog.Start(parentCtx, "handle.request")
	wlog.SetGroup(parentCtx, "trace", "request_id", "req-1", "trace_id", "trace-1")

	childCtx, childEnd := wlog.Detach(parentCtx, "send.email")
	childOut := captureStdout(t, childEnd)
	parentOut := captureStdout(t, parentEnd)

	var child map[string]any
	if err := json.Unmarshal([]byte(childOut), &child); err != nil {
		t.Fatalf("child: invalid JSON line: %v\noutput: %q", err, childOut)
	}
	trace, ok := child["trace"].(map[string]any)
	if !ok {
		t.Fatalf("child trace missing: %v", child["trace"])
	}
	if trace["request_id"] != "req-1" || trace["trace_id"] != "trace-1" {
		t.Errorf("child did not inherit parent's trace ids: %v", trace)
	}
	if trace["parent_operation"] != "handle.request" {
		t.Errorf("trace.parent_operation = %v, want handle.request", trace["parent_operation"])
	}
	if child["operation"] != "send.email" {
		t.Errorf("child operation = %v, want send.email", child["operation"])
	}

	var parent map[string]any
	if err := json.Unmarshal([]byte(parentOut), &parent); err != nil {
		t.Fatalf("parent: invalid JSON line: %v\noutput: %q", err, parentOut)
	}
	if parent["operation"] != "handle.request" {
		t.Errorf("parent operation = %v, want handle.request", parent["operation"])
	}
	_ = childCtx
}

func TestCore_LateWrite_CountedOnOpenParent(t *testing.T) {
	log := wlog.New()
	parentCtx := log.WithContext(context.Background())
	parentCtx, parentEnd := wlog.Start(parentCtx, "parent.op")
	childCtx, childEnd := wlog.Detach(parentCtx, "child.op")

	captureStdout(t, childEnd) // seals and emits the child

	// The child is already sealed; this write must not panic, and must surface on
	// the nearest still-open ancestor (the parent) instead of vanishing silently.
	wlog.Set(childCtx, "late", "value")

	parentOut := captureStdout(t, parentEnd)
	var parent map[string]any
	if err := json.Unmarshal([]byte(parentOut), &parent); err != nil {
		t.Fatalf("invalid JSON line: %v\noutput: %q", err, parentOut)
	}
	if parent["wlog.late_writes"] != float64(1) {
		t.Errorf("parent wlog.late_writes = %v, want 1", parent["wlog.late_writes"])
	}
}

func TestCore_LateWrite_NoOpenAncestor_IsSilentNoop(t *testing.T) {
	log := wlog.New()
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "solo.op")
	end() // seals; no parent exists at all

	// Must not panic.
	wlog.Set(ctx, "late", "value")
	wlog.SetGroup(ctx, "g", "k", "v")
	wlog.Append(ctx, "arr", 1)
}
