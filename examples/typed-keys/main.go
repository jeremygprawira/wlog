// Command typed-keys shows wlog.Key, a field name with its value type checked at
// compile time, and StrictKeys, which flags a typo'd field name in local/dev.
package main

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

var (
	// OrderID is a string field.
	OrderID = wlog.NewKey[string]("order_id")
	// Amount is a float64 field.
	Amount = wlog.NewKey[float64]("amount")
)

func main() {
	log := wlog.New(
		wlog.StrictKeys(OrderID, Amount),
		wlog.WithService("typed-keys-example", "0.0.1", "local"),
	)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "order.create")
	OrderID.Set(ctx, "ord-1")
	Amount.Set(ctx, 12.50)
	wlog.Set(ctx, "amout", 1.0) // typo: recorded in wlog.unknown_keys
	end()
}
