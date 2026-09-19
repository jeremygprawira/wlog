// Command audit-refund records a refund as an audit fact on the request's event, writes
// the event to a tamper-evident journal, and verifies that journal.
//
// The audit fact is never sampled away, so the refund reaches the journal even when a
// sampler drops every ordinary event.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/sample"
)

// journalKey signs the example journal, so a reader needs the key to accept a record.
var journalKey = []byte("audit-refund-example-key")

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

// Run records one refund, appends it to the signed journal at path, and verifies the
// chain. It returns the first failure, so a tampered journal surfaces here.
func Run(path string) error {
	log := wlog.New(
		wlog.WithService("audit-refund-example", "0.0.1", "local"),
		// A head sampler that keeps no ordinary event proves the audit record still lands.
		wlog.WithHeadSampler(sample.MustNew(sample.Rate(wlog.LevelInfo, 0))),
		wlog.WithDrains(audit.Journal(path, audit.WithKey(journalKey))),
	)
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "refund.approve")
	refund(ctx, "ord-1", 12.50)
	end()

	// Close writes the journal's closing marker, so Verify can check the whole chain.
	if err := log.Close(context.Background()); err != nil {
		return err
	}
	return audit.Verify(path, journalKey)
}

func main() {
	path := "audit-refund.ndjson"
	if err := Run(path); err != nil {
		fmt.Fprintln(os.Stderr, "audit-refund:", err)
		os.Exit(1)
	}
	fmt.Println("refund recorded and journal verified:", path)
}
