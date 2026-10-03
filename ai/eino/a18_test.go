package wlogeino

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

func recordedMessage(t *testing.T) *schema.Message {
	t.Helper()
	body, err := os.ReadFile("testdata/message.json")
	if err != nil {
		t.Fatal(err)
	}
	var msg schema.Message
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	return &msg
}

// TestEino_A18_StreamRunsOffTheCaller proves the stream callback returns
// before the stream ends. The drain runs in its own goroutine.
func TestEino_A18_StreamRunsOffTheCaller(t *testing.T) {
	log, _ := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	defer end()
	reader, writer := schema.Pipe[callbacks.CallbackOutput](0)
	defer writer.Close()
	h := Handler()
	done := make(chan struct{})
	go func() {
		h.OnEndWithStreamOutput(ctx, &callbacks.RunInfo{Component: components.ComponentOfChatModel}, reader)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("stream callback blocked the caller")
	}
}

// TestEino_A18_DrainClosesStream proves drain closes the reader when the
// stream ends, using a message the SDK recorded.
func TestEino_A18_DrainClosesStream(t *testing.T) {
	log, _ := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	defer end()
	reader, writer := schema.Pipe[*model.CallbackOutput](1)
	msg := recordedMessage(t)
	if closed := writer.Send(&model.CallbackOutput{Message: msg}, nil); closed {
		t.Fatal("send closed before drain")
	}
	writer.Close()
	drain(ctx, reader, "eino", false)
	func() {
		defer func() {
			if recover() != nil {
				t.Error("reader was still open after drain")
			}
		}()
		if closed := writer.Send(&model.CallbackOutput{Message: msg}, nil); !closed {
			t.Error("reader was still open after drain")
		}
	}()
}

// TestEino_A18_RecoveredPanicStaysInside proves a panic in the drain does
// not escape the goroutine.
func TestEino_A18_RecoveredPanicStaysInside(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("drain panic escaped: %v", r)
		}
	}()
	drain(context.Background(), nil, "eino", false)
}
