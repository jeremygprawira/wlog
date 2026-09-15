package wlogzerolog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jeremygprawira/wlog"
	wlogzerolog "github.com/jeremygprawira/wlog/log/zerolog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// captureDrain wires a wlogtest logger whose events also drain through a zerolog logger
// into buf, then returns the parsed JSON and the zerolog level it carried.
func captureDrain(t *testing.T, emit func(ctx context.Context)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	log, _ := wlogtest.New(t, wlog.WithDrains(wlogzerolog.Drain(zerolog.New(&buf))))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	emit(ctx)
	end()

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("zerolog output is not one JSON record: %v\n%q", err, buf.String())
	}
	return rec
}

// TestZerologOutput_OneRecordWithNestedDict proves one wlog event becomes one zerolog
// record, message from operation, level mapped, and a nested map kept as an object.
func TestZerologOutput_OneRecordWithNestedDict(t *testing.T) {
	rec := captureDrain(t, func(ctx context.Context) {
		wlog.SetGroup(ctx, "http", "status", 200)
		wlog.Set(ctx, "user_id", "u1")
	})

	if rec["message"] != "op" || rec["level"] != "info" {
		t.Errorf("message/level = %v/%v, want op/info", rec["message"], rec["level"])
	}
	http, _ := rec["http"].(map[string]any)
	if http["status"] != float64(200) {
		t.Errorf("nested http.status = %v, want 200", rec["http"])
	}
	if rec["user_id"] != "u1" {
		t.Errorf("user_id = %v, want u1", rec["user_id"])
	}
}

// TestZerologOutput_LevelMapping proves each wlog level maps to the matching zerolog
// level string.
func TestZerologOutput_LevelMapping(t *testing.T) {
	cases := map[wlog.Level]string{
		wlog.LevelDebug: "debug",
		wlog.LevelInfo:  "info",
		wlog.LevelWarn:  "warn",
		wlog.LevelError: "error",
	}
	for in, want := range cases {
		rec := captureDrain(t, func(ctx context.Context) { wlog.SetLevel(ctx, in) })
		if rec["level"] != want {
			t.Errorf("wlog level %v mapped to %v, want %v", in, rec["level"], want)
		}
	}
}
