package wloglogrus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/jeremygprawira/wlog"
	wloglogrus "github.com/jeremygprawira/wlog/log/logrus"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// captureDrain wires a wlogtest logger whose events also drain through a logrus logger
// into buf, then returns the parsed JSON record.
func captureDrain(t *testing.T, emit func(ctx context.Context)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := logrus.New()
	logger.Out = &buf
	logger.Level = logrus.DebugLevel
	logger.Formatter = &logrus.JSONFormatter{}
	log, _ := wlogtest.New(t, wlog.WithDrains(wloglogrus.Drain(logger)))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	emit(ctx)
	end()

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("logrus output is not one JSON record: %v\n%q", err, buf.String())
	}
	return rec
}

// TestLogrusOutput_FlattensNestedGroups proves one wlog event becomes one logrus record,
// message from operation, level mapped, and a nested map flattened to dotted keys,
// since logrus has no native nesting.
func TestLogrusOutput_FlattensNestedGroups(t *testing.T) {
	rec := captureDrain(t, func(ctx context.Context) {
		wlog.SetGroup(ctx, "http", "status", 200)
		wlog.Set(ctx, "user_id", "u1")
	})

	if rec["msg"] != "op" || rec["level"] != "info" {
		t.Errorf("msg/level = %v/%v, want op/info", rec["msg"], rec["level"])
	}
	if rec["http.status"] != float64(200) {
		t.Errorf("flattened http.status = %v, want 200", rec["http.status"])
	}
	if _, nested := rec["http"]; nested {
		t.Errorf("logrus record still has a nested http object: %v", rec["http"])
	}
	if rec["user_id"] != "u1" {
		t.Errorf("user_id = %v, want u1", rec["user_id"])
	}
}

// TestLogrusOutput_LevelMapping proves each wlog level maps to the matching logrus
// level (logrus spells warn as "warning").
func TestLogrusOutput_LevelMapping(t *testing.T) {
	cases := map[wlog.Level]string{
		wlog.LevelDebug: "debug",
		wlog.LevelInfo:  "info",
		wlog.LevelWarn:  "warning",
		wlog.LevelError: "error",
	}
	for in, want := range cases {
		rec := captureDrain(t, func(ctx context.Context) { wlog.SetLevel(ctx, in) })
		if rec["level"] != want {
			t.Errorf("wlog level %v mapped to %v, want %v", in, rec["level"], want)
		}
	}
}
