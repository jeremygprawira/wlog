// This file drives the redis hook over miniredis, so the tests need no real server. It
// checks the call record of one command, the miss status, the error prefix, the pipeline
// count, and a command outside a unit of work.
package wlogredis_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogredis "github.com/jeremygprawira/wlog/store/redis"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestRedis_C1_Command proves that one command gives one cache call record.
func TestRedis_C1_Command(t *testing.T) {
	log, rec := wlogtest.New(t)
	rdb := newRedis(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if err := rdb.Set(ctx, "session:1", "secret-value", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	for key, want := range map[string]any{
		"kind": "cache", "system": "redis", "operation": "set", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "secret-value") {
		t.Errorf("the value reached the event: %s", body)
	}
}

// TestRedis_C1_Miss proves that a missing key records the status miss and no error.
func TestRedis_C1_Miss(t *testing.T) {
	log, rec := wlogtest.New(t)
	rdb := newRedis(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	err := rdb.Get(ctx, "missing").Err()
	end()
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("get error = %v, want redis.Nil", err)
	}

	record := onlyCall(t, rec)
	if record["status"] != "miss" {
		t.Errorf("calls[0].status = %v, want miss", record["status"])
	}
	if _, present := record["error"]; present {
		t.Errorf("calls[0].error = %v, want no error", record["error"])
	}
}

// TestRedis_B2_ErrorCode proves that a command error records its prefix and never its
// text.
func TestRedis_B2_ErrorCode(t *testing.T) {
	log, rec := wlogtest.New(t)
	rdb := newRedis(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if err := rdb.Do(ctx, "NOTACOMMAND", "x").Err(); err == nil {
		t.Fatal("the command returned no error")
	}
	end()

	record := onlyCall(t, rec)
	callError, _ := record["error"].(map[string]any)
	if callError["code"] != "ERR" {
		t.Errorf("calls[0].error.code = %v, want ERR", callError["code"])
	}
	if body, err := json.Marshal(rec.Last()); err == nil && strings.Contains(string(body), "unknown command") {
		t.Errorf("the error text reached the event: %s", body)
	}
}

// TestRedis_B2_Pipeline proves that one pipeline gives one call with its command count.
func TestRedis_B2_Pipeline(t *testing.T) {
	log, rec := wlogtest.New(t)
	rdb := newRedis(t)

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	if _, err := rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, "a", "1", 0)
		pipe.Get(ctx, "a")
		return nil
	}); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	end()

	record := onlyCall(t, rec)
	if record["operation"] != "pipeline" {
		t.Errorf("calls[0].operation = %v, want pipeline", record["operation"])
	}
	if !conformance.Equal(record["rows"], int64(2)) {
		t.Errorf("calls[0].rows = %v, want 2", record["rows"])
	}
}

// TestRedis_B2_NoEvent proves that a command outside a unit of work records nothing and
// reports nothing.
func TestRedis_B2_NoEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	rdb.AddHook(wlogredis.Hook())

	if err := rdb.Set(rec.Logger().WithContext(context.Background()), "k", "v", 0).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if count := len(rec.Events()); count != 0 {
		t.Errorf("events = %d, want none", count)
	}
	if count := len(rec.Problems()); count != 0 {
		t.Errorf("problems = %d, want none", count)
	}
}

// newRedis starts one miniredis server and one client with the wlog hook.
func newRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	rdb.AddHook(wlogredis.Hook())
	return rdb
}

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
