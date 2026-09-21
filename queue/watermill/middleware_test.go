// This file runs the work conformance suite against the event path, and checks the poison
// queue rule that only watermill has.
package wlogwatermill

import (
	"context"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	watermillmiddleware "github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestWatermill_C1_WorkConformance proves that the event path passes every scenario of the
// work suite.
func TestWatermill_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path of this adapter. The suite supplies the
// unit, because a Watermill message carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestWatermill_C3_PoisonQueueDeadLetter proves that a handler which fails into the poison
// queue records result dead_letter, level error, and the fields of the message.
func TestWatermill_C3_PoisonQueueDeadLetter(t *testing.T) {
	pubSub := gochannel.NewGoChannel(gochannel.Config{}, watermill.NopLogger{})
	defer pubSub.Close()
	log, rec := wlogtest.New(t)

	router, err := message.NewRouter(message.RouterConfig{CloseTimeout: time.Second}, watermill.NopLogger{})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	router.AddMiddleware(Middleware(log))
	poison, err := watermillmiddleware.PoisonQueue(pubSub, "poison")
	if err != nil {
		t.Fatalf("PoisonQueue: %v", err)
	}
	router.AddMiddleware(poison)
	router.AddHandler("orders", "orders", pubSub, "", pubSub, func(*message.Message) ([]*message.Message, error) {
		return nil, errString("handler failed")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = router.Run(ctx) }()
	<-router.Running()
	defer func() { _ = router.Close() }()

	// Subscribe before the publish, because the go channel drops a message that has no
	// subscriber.
	poisoned, err := pubSub.Subscribe(context.Background(), "poison")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := pubSub.Publish("orders", message.NewMessage("msg-1", nil)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case <-poisoned:
	case <-time.After(2 * time.Second):
		t.Fatal("the poison topic received no message")
	}

	got := waitForEvent(t, rec)
	if got["level"] != "error" {
		t.Errorf("level = %v, want error", got["level"])
	}
	if got["operation"] != "process orders" {
		t.Errorf("operation = %v, want process orders", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "watermill", "operation": "process", "destination": "orders",
		"message_id": "msg-1", "result": "dead_letter",
	} {
		if messaging[key] != want {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	watermillFields, _ := messaging["watermill"].(map[string]any)
	if watermillFields["handler"] != "orders" {
		t.Errorf("messaging.watermill.handler = %v, want orders", watermillFields["handler"])
	}
}

// waitForEvent waits until the recorder holds one event, because the router runs the handler
// on its own goroutine.
func waitForEvent(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	for i := 0; i < 400; i++ {
		if event := rec.Last(); event != nil {
			return event
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no event recorded")
	return nil
}

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }

// process runs one unit of work through the event path with a recovered panic, so the
// conformance suite continues after the panic scenario. The real entries record a panic and
// raise it again, which is the rule of the track spec.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}
