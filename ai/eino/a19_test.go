package wlogeino

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func chatInfo() *callbacks.RunInfo {
	return &callbacks.RunInfo{Type: "OpenAI", Component: components.ComponentOfChatModel}
}

// TestEino_A19_CancelStopsDrain proves a stream that never closes does not
// keep the call's goroutine. Cancelling the caller ends the detached event.
func TestEino_A19_CancelStopsDrain(t *testing.T) {
	log, rec := wlogtest.New(t)
	parent, end := wlog.Start(log.WithContext(context.Background()), "op")
	ctx, cancel := context.WithCancel(parent)
	reader, writer := schema.Pipe[callbacks.CallbackOutput](0)
	t.Cleanup(func() { writer.Close() })
	Handler().OnEndWithStreamOutput(ctx, chatInfo(), reader)
	cancel()
	end()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(rec.Events()) >= 2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("events = %d, want the detached event after cancel", len(rec.Events()))
}

// TestEino_A19_FailedStreamIsMarked proves a stream error is not a quiet
// info event with a zero token count.
func TestEino_A19_FailedStreamIsMarked(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	writer.Send((*model.CallbackOutput)(nil), errors.New("midway"))
	writer.Close()
	drain(ctx, ctx, reader, "openai", false)
	end()

	ev := rec.Last()
	if ev["level"] != "error" {
		t.Fatalf("level = %v, want error", ev["level"])
	}
	group, _ := ev["llm"].(map[string]any)
	reasons, _ := group["finish_reasons"].([]any)
	if len(reasons) != 1 || reasons[0] != "error" {
		t.Fatalf("finish_reasons = %v, want [error]", reasons)
	}
	if ev["error"] == nil {
		t.Fatal("error is missing")
	}
}

// TestEino_A19_OnErrorIsRecorded proves a model error callback is an event
// error, not a dropped call.
func TestEino_A19_OnErrorIsRecorded(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	Handler().OnError(ctx, chatInfo(), errors.New("model down"))
	end()
	if rec.Last()["level"] != "error" || rec.Last()["error"] == nil {
		t.Fatalf("event = %v, want level error and an error field", rec.Last())
	}
}

// TestEino_A19_PanicIsLogged proves a recovered drain panic is on the event.
func TestEino_A19_PanicIsLogged(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	drain(ctx, ctx, nil, "eino", false)
	end()
	errInfo, _ := rec.Last()["error"].(map[string]any)
	msg, _ := errInfo["message"].(string)
	if msg == "" {
		t.Fatalf("error = %v, want a panic message", rec.Last()["error"])
	}
}
