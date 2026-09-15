// Command audit-refund records a refund as an audit fact on the request's event. The
// audit field is never sampled away, so the refund fact always reaches the drains.
package main

import (
	"context"
	"fmt"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
)

// refund records who refunded what, and adds the order to the event.
func refund(ctx context.Context, orderID string, amount float64) {
	wlog.Set(ctx, "order_id", orderID)
	wlog.Set(ctx, "amount", amount)
	audit.Do(ctx, audit.Record{
		Actor:   audit.Actor{Type: "user", ID: "u-42"},
		Action:  "refund.create",
		Target:  audit.Target{Type: "order", ID: orderID},
		Outcome: "success",
	})
}

func main() {
	log := wlog.New(wlog.WithService("audit-refund-example", "0.0.1", "local"))
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "http.request")
	refund(ctx, "ord-1", 12.50)
	end()

	fmt.Println("refund recorded")
}
