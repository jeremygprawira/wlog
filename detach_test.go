package wlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
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
	var ctx context.Context
	var end func()
	captureStdout(t, func() {
		ctx = log.WithContext(context.Background())
		ctx, end = wlog.Start(ctx, "solo.op")
		end() // seals; no parent exists at all
	})

	// Must not panic.
	wlog.Set(ctx, "late", "value")
	wlog.SetGroup(ctx, "g", "k", "v")
	wlog.Append(ctx, "arr", 1)
}

// TestCore_CORE10_DetachSurvivesParentCancel proves that a detached event is not
// canceled with its parent, and that it keeps the values the parent context
// carried.
func TestCore_CORE10_DetachSurvivesParentCancel(t *testing.T) {
	log, rec := wlogtest.New(t)
	parent, cancel := context.WithCancel(log.WithContext(context.Background()))

	ctx, end := wlog.Start(parent, "request")
	child, childEnd := wlog.Detach(ctx, "email")
	cancel()

	if err := child.Err(); err != nil {
		t.Fatalf("the detached context is canceled: %v", err)
	}
	wlog.Set(child, "user_id", "u1")
	childEnd()
	end()

	// The child ends first, so its event is not the last one; find it by name.
	var child_event map[string]any
	for _, ev := range rec.Events() {
		if ev["operation"] == "email" {
			child_event = ev
		}
	}
	if child_event == nil {
		t.Fatalf("no detached event: %v", rec.Events())
	}
	if child_event["user_id"] != "u1" {
		t.Errorf("the detached event lost a write: %v", child_event)
	}
	trace, _ := child_event["trace"].(map[string]any)
	if trace["parent_operation"] != "request" {
		t.Errorf("trace.parent_operation = %v, want request", trace["parent_operation"])
	}
}

// TestCore_CORE28_DetachAppliesStrictKeys proves that a detached event inherits
// the key checking of its parent's environment, so a typo is still reported.
func TestCore_CORE28_DetachAppliesStrictKeys(t *testing.T) {
	log, rec := wlogtest.New(t,
		wlog.StrictKeys(wlog.NewKey[string]("order_id")),
		wlog.WithService("svc", "1.0.0", "local"),
	)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "request")
	child, childEnd := wlog.Detach(ctx, "email")
	wlog.Set(child, "amout", 1.0)
	childEnd()
	end()

	found := false
	for _, ev := range rec.Events() {
		if keys, ok := ev["wlog.unknown_keys"].([]any); ok {
			for _, k := range keys {
				if k == "amout" {
					found = true
				}
			}
		}
		if keys, ok := ev["wlog.unknown_keys"].([]string); ok {
			for _, k := range keys {
				if k == "amout" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("the detached event did not report the typo: %v", rec.Events())
	}
}
