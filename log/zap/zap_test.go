package wlogzap_test

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/jeremygprawira/wlog"
	wlogzap "github.com/jeremygprawira/wlog/log/zap"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestZapOutput_OneEntryWithNestedField proves one wlog event becomes one zap entry,
// with operation as the message, level mapped, and a nested map kept as an object.
func TestZapOutput_OneEntryWithNestedField(t *testing.T) {
	core, recorded := observer.New(zap.DebugLevel)
	log, _ := wlogtest.New(t, wlog.WithDrains(wlogzap.Drain(zap.New(core))))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	wlog.SetGroup(ctx, "http", "status", 200)
	wlog.Set(ctx, "user_id", "u1")
	end()

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("got %d zap entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Level != zap.InfoLevel || entry.Message != "op" {
		t.Errorf("entry level/message = %v/%q, want info/op", entry.Level, entry.Message)
	}
	fields := entry.ContextMap()
	httpFields, _ := fields["http"].(map[string]any)
	if httpFields["status"] != 200 {
		t.Errorf("nested http.status = %v, want 200", fields["http"])
	}
	if fields["user_id"] != "u1" {
		t.Errorf("user_id = %v, want u1", fields["user_id"])
	}
}

// TestZapOutput_LevelMapping proves each wlog level maps to the matching zap level.
func TestZapOutput_LevelMapping(t *testing.T) {
	cases := map[wlog.Level]zapcore.Level{
		wlog.LevelDebug: zap.DebugLevel,
		wlog.LevelInfo:  zap.InfoLevel,
		wlog.LevelWarn:  zap.WarnLevel,
		wlog.LevelError: zap.ErrorLevel,
	}
	for in, want := range cases {
		core, recorded := observer.New(zap.DebugLevel)
		log, _ := wlogtest.New(t, wlog.WithDrains(wlogzap.Drain(zap.New(core))))
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.SetLevel(ctx, in)
		end()

		if got := recorded.All()[0].Level; got != want {
			t.Errorf("wlog level %v mapped to %v, want %v", in, got, want)
		}
	}
}
