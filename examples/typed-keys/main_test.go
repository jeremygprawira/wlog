package main

import (
	"context"
	"slices"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestTypedKeys_SetAndFlagTypo proves a typed key writes its value, and an untyped
// write with a name outside the key list is recorded in wlog.unknown_keys in a local
// environment.
func TestTypedKeys_SetAndFlagTypo(t *testing.T) {
	log, rec := wlogtest.New(t,
		wlog.StrictKeys(OrderID, Amount),
		wlog.WithService("typed-keys-example", "0.0.1", "local"),
	)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "order.create")
	OrderID.Set(ctx, "ord-1")
	Amount.Set(ctx, 12.50)
	wlog.Set(ctx, "amout", 1.0)
	end()

	last := rec.Last()
	if last["order_id"] != "ord-1" {
		t.Errorf("order_id = %v, want ord-1", last["order_id"])
	}
	if last["amount"] != 12.50 {
		t.Errorf("amount = %v, want 12.5", last["amount"])
	}
	unknowns, _ := counters(last)["unknown_keys"].([]string)
	if !slices.Contains(unknowns, "amout") {
		t.Errorf("wlog.unknown_keys = %v, want it to contain amout", unknowns)
	}
}

// counters returns the nested wlog object of an event, which holds the counters of what
// core had to drop.
func counters(event map[string]any) map[string]any {
	object, _ := event["wlog"].(map[string]any)
	return object
}
