package llm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLLM_L9_AddWritesV2FieldsAndCall proves Add stores the v2 record fields and one
// calls entry the query can rank.
func TestLLM_L9_AddWritesV2FieldsAndCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "chat")
	llm.Add(ctx, llm.Record{
		Provider:      "anthropic",
		Model:         "claude-sonnet-4-6",
		ResponseModel: "claude-sonnet-4-6-20260101",
		Operation:     "chat",
		Status:        "completed",
		FinishReasons: []string{"stop"},
		Attempts:      2,
		RequestIDs:    []string{"req_1"},
		Err:           errors.New("boom"),
		ToolCalls:     []llm.ToolCall{{ID: "call_1", Name: "lookup"}},
		Duration:      time.Second,
	})
	end()

	event := rec.Last()
	group, _ := event["llm"].(map[string]any)
	if group["response_model"] != "claude-sonnet-4-6-20260101" {
		t.Errorf("response_model = %v", group["response_model"])
	}
	if group["status"] != "completed" {
		t.Errorf("status = %v", group["status"])
	}
	reasons, _ := group["finish_reasons"].([]any)
	if len(reasons) != 1 || reasons[0] != "stop" {
		t.Errorf("finish_reasons = %v, want [stop]", group["finish_reasons"])
	}
	if fmt.Sprint(group["attempts"]) != "2" {
		t.Errorf("attempts = %v, want 2", group["attempts"])
	}
	ids, _ := group["request_ids"].([]any)
	if len(ids) != 1 || ids[0] != "req_1" {
		t.Errorf("request_ids = %v, want [req_1]", group["request_ids"])
	}
	errObj, _ := group["error"].(map[string]any)
	if errObj["message"] != "boom" {
		t.Errorf("error = %v, want message boom", group["error"])
	}
	tools, _ := group["tool_calls"].([]any)
	tool, _ := tools[0].(map[string]any)
	if tool["id"] != "call_1" {
		t.Errorf("tool id = %v, want call_1", tool["id"])
	}
	llmCalls, _ := group["calls"].([]any)
	if len(llmCalls) != 1 {
		t.Fatalf("llm.calls = %d, want 1", len(llmCalls))
	}
	calls, _ := event["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	call, _ := calls[0].(map[string]any)
	if call["kind"] != "llm" || call["system"] != "anthropic" || call["operation"] != "chat" || call["target"] != "claude-sonnet-4-6-20260101" {
		t.Errorf("call = %v, want an llm call for the response model", call)
	}
}
