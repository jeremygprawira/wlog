// This file covers the core helpers the batch F and G work added or exposed: the group
// update under the event lock, the dropped count, the JSON tree, and the reserved-key
// helpers.
package wlog_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// num reads a whole number whatever width the event holds it in.
func num(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	}
	return -1
}

// TestCore_L1_UpdateGroupUnderTheEventLock proves the update runs on the live group and
// creates it when it is missing.
func TestCore_L1_UpdateGroupUnderTheEventLock(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	wlog.UpdateGroup(ctx, "llm", func(group map[string]any) {
		group["input_tokens"] = 3
	})
	wlog.UpdateGroup(ctx, "llm", func(group map[string]any) {
		group["output_tokens"] = 4
	})
	wlog.CountDropped(ctx, 2)
	end()

	event := rec.Last()
	group, ok := event["llm"].(map[string]any)
	if !ok {
		t.Fatalf("llm group = %v, want a map", event["llm"])
	}
	if num(group["input_tokens"]) != 3 || num(group["output_tokens"]) != 4 {
		t.Errorf("llm group = %v, want both counts", group)
	}
	wlogFields, _ := event["wlog"].(map[string]any)
	if got := num(wlogFields["dropped_fields"]); got != 2 {
		t.Errorf("wlog.dropped_fields = %v, want 2", got)
	}
}

// TestCore_L1_HelpersAreUsable proves the JSON tree, the reserved key test, the reserved
// field list, and the current error all work outside an event.
func TestCore_L1_HelpersAreUsable(t *testing.T) {
	tree, ok := wlog.JSONTree(`{"a":{"b":1}}`).(map[string]any)
	if !ok {
		t.Fatalf("JSONTree = %T, want a map", wlog.JSONTree(`{"a":{"b":1}}`))
	}
	if _, ok := tree["a"].(map[string]any); !ok {
		t.Errorf("JSONTree did not build the nested map: %v", tree)
	}

	if !wlog.IsReservedKey("trace") {
		t.Error("IsReservedKey(trace) = false, want true")
	}
	if wlog.IsReservedKey("a_user_key") {
		t.Error("IsReservedKey(a_user_key) = true, want false")
	}
	if len(wlog.ReservedFields()) == 0 {
		t.Error("ReservedFields() is empty")
	}

	log, _ := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Error(ctx, context.Canceled)
	if _, ok := wlog.CurrentError(ctx); !ok {
		t.Error("CurrentError found no error after wlog.Error")
	}
	end()
}
