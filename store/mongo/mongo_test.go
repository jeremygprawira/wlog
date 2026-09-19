// This file drives the mongo monitor without a server: the monitor callbacks take plain
// events, so a test calls them the way the driver would. It checks the call record of a
// command, the driver duration, the error code, the collection option, and the forwarding
// to the monitor of the app.
package wlogmongo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogmongo "github.com/jeremygprawira/wlog/store/mongo"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestMongo_C1_CommandCall proves that one succeeded command gives one db call record
// with the name, the database, the row count, and the duration of the driver.
func TestMongo_C1_CommandCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	monitor := wlogmongo.Monitor(nil)
	if monitor.Started != nil {
		t.Error("the default monitor sets Started, which makes the driver copy every command")
	}
	reply, err := bson.Marshal(bson.M{"n": 3})
	if err != nil {
		t.Fatalf("marshal the reply: %v", err)
	}

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	monitor.Succeeded(ctx, &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: 25 * time.Millisecond, CommandName: "insert", DatabaseName: "shop", RequestID: 7,
		},
		Reply: bson.Raw(reply),
	})
	end()

	record := onlyCall(t, rec)
	for key, want := range map[string]any{
		"kind": "db", "system": "mongodb", "operation": "insert", "target": "shop", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	if !conformance.Equal(record["rows"], int64(3)) {
		t.Errorf("calls[0].rows = %v, want 3", record["rows"])
	}
	if record["duration_ms"] != float64(25) {
		t.Errorf("calls[0].duration_ms = %v, want the 25ms of the driver", record["duration_ms"])
	}
}

// TestMongo_B2_FailedCall proves that a failed command records a code and never the error
// text.
func TestMongo_B2_FailedCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	monitor := wlogmongo.Monitor(nil)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	monitor.Failed(ctx, &event.CommandFailedEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: 5 * time.Millisecond, CommandName: "find", DatabaseName: "shop",
		},
		Failure: mongo.CommandError{Code: 13, Name: "Unauthorized", Message: "not authorized on shop"},
	})
	end()

	record := onlyCall(t, rec)
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "Unauthorized" {
		t.Errorf("calls[0].error.code = %v, want Unauthorized", callError["code"])
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "not authorized") {
		t.Errorf("the error text reached the event: %s", body)
	}
}

// TestMongo_B2_WithCollection proves that the collection option reads the command once,
// names the target database.collection, and forwards every callback of the app.
func TestMongo_B2_WithCollection(t *testing.T) {
	log, rec := wlogtest.New(t)
	next := &recordingMonitor{}
	monitor := wlogmongo.Monitor(&event.CommandMonitor{
		Started: next.started, Succeeded: next.succeeded, Failed: next.failed,
	}, wlogmongo.WithCollection())
	if monitor.Started == nil {
		t.Fatal("the collection option left Started nil")
	}
	command, err := bson.Marshal(bson.M{"insert": "orders", "documents": []any{bson.M{"id": 1}}})
	if err != nil {
		t.Fatalf("marshal the command: %v", err)
	}

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	monitor.Started(ctx, &event.CommandStartedEvent{
		Command: command, DatabaseName: "shop", CommandName: "insert", RequestID: 7, ConnectionID: "c1",
	})
	monitor.Succeeded(ctx, &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: time.Millisecond, CommandName: "insert", DatabaseName: "shop", RequestID: 7, ConnectionID: "c1",
		},
	})
	end()

	record := onlyCall(t, rec)
	if record["target"] != "shop.orders" {
		t.Errorf("calls[0].target = %v, want shop.orders", record["target"])
	}
	if next.starts != 1 || next.succeeds != 1 || next.fails != 0 {
		t.Errorf("the wrapped monitor saw %d starts, %d successes, %d failures, want 1, 1, 0",
			next.starts, next.succeeds, next.fails)
	}
}

// TestMongo_B2_NoEvent proves that a command outside a unit of work records nothing and
// reports nothing.
func TestMongo_B2_NoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	monitor := wlogmongo.Monitor(nil)

	ctx := rec.Logger().WithContext(context.Background())
	monitor.Succeeded(ctx, &event.CommandSucceededEvent{})
	monitor.Failed(ctx, &event.CommandFailedEvent{})

	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// recordingMonitor counts the callbacks of the app.
type recordingMonitor struct {
	starts, succeeds, fails int
}

// started counts one start.
func (r *recordingMonitor) started(context.Context, *event.CommandStartedEvent) { r.starts++ }

// succeeded counts one success.
func (r *recordingMonitor) succeeded(context.Context, *event.CommandSucceededEvent) { r.succeeds++ }

// failed counts one failure.
func (r *recordingMonitor) failed(context.Context, *event.CommandFailedEvent) { r.fails++ }

// onlyCall returns the only call record of the last event.
func onlyCall(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	return record
}
