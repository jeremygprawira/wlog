package wlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremygprawira/wlog"
)

func TestCore_Key_SetStoresUnderItsName(t *testing.T) {
	orderID := wlog.NewKey[string]("order_id")
	out := captureStdout(t, func() {
		log := wlog.New()
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		orderID.Set(ctx, "4821")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if got["order_id"] != "4821" {
		t.Errorf("order_id = %v, want 4821", got["order_id"])
	}
}

func TestCore_StrictKeys_FlagsUnregisteredKey_InDev(t *testing.T) {
	orderID := wlog.NewKey[string]("order_id")
	log := wlog.New(
		wlog.WithService("svc", "1.0.0", "dev"),
		wlog.StrictKeys(orderID),
		wlog.WithFormat(wlog.FormatJSON),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		orderID.Set(ctx, "4821") // registered: fine
		wlog.Set(ctx, "typo_field", "oops")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	unknown, ok := got["wlog.unknown_keys"].([]any)
	if !ok || len(unknown) != 1 || unknown[0] != "typo_field" {
		t.Errorf("wlog.unknown_keys = %v, want [typo_field]", got["wlog.unknown_keys"])
	}
}

func TestCore_StrictKeys_NoOp_OutsideLocalDev(t *testing.T) {
	orderID := wlog.NewKey[string]("order_id")
	log := wlog.New(
		wlog.WithService("svc", "1.0.0", "prod"),
		wlog.StrictKeys(orderID),
	)

	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "typo_field", "oops")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if _, ok := got["wlog.unknown_keys"]; ok {
		t.Errorf("wlog.unknown_keys present in prod: %v", got["wlog.unknown_keys"])
	}
}

func TestCore_NoStrictKeys_NeverFlags(t *testing.T) {
	log := wlog.New(wlog.WithService("svc", "1.0.0", "dev"), wlog.WithFormat(wlog.FormatJSON))
	out := captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "anything", "value")
		end()
	})

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	if _, ok := got["wlog.unknown_keys"]; ok {
		t.Errorf("wlog.unknown_keys present without StrictKeys configured: %v", got["wlog.unknown_keys"])
	}
}
