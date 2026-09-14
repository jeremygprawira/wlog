package wlog_test

import (
	"context"
	"errors"

	"github.com/jeremygprawira/wlog"
)

// Example demonstrates the core usage pattern: build a Logger, attach it to a
// context, and wrap one unit of work in Start/end. Password is masked by the default
// redactor; the timestamp, duration, and redact.fingerprint are omitted below since
// they vary from run to run.
func Example() {
	log := wlog.New(wlog.WithService("go-customer", "1.4.0", "prod"), wlog.WithFormat(wlog.FormatJSON))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "order.create")
	wlog.Set(ctx, "order_id", "4821")
	wlog.Set(ctx, "password", "hunter2")
	end()

	// Output is one JSON line: {"duration_ms":0,"level":"info","operation":"order.create",
	// "order_id":"4821","outcome":"success","password":"[REDACTED]", ...}
}

func ExampleDetach() {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "handle.request")
	defer end()

	// Work that must keep running after the request's own event is emitted.
	go func(ctx context.Context) {
		ctx, end := wlog.Detach(ctx, "send.receipt.email")
		defer end()
		wlog.Set(ctx, "provider", "ses")
	}(ctx)
}

func ExampleError() {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "order.get")
	defer end()

	if _, err := findOrder("bad-id"); err != nil {
		wlog.Error(ctx, err)
	}
	// The event's level and outcome become "error" automatically.
}

func findOrder(string) (string, error) {
	return "", errors.New("order not found")
}

func ExampleNewKey() {
	var OrderID = wlog.NewKey[string]("order_id")

	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "order.create")
	OrderID.Set(ctx, "4821") // OrderID.Set(ctx, 4821) would not compile: not a string
	end()
}

func ExampleWithDrains() {
	seen := 0
	countingDrain := wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		seen++
	})

	log := wlog.New(wlog.WithFormat(wlog.FormatJSON), wlog.WithDrains(countingDrain))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	end()

	_ = seen // every drain, alongside the default stdout sink, receives the event
}

func ExampleInfo() {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	ctx := log.WithContext(context.Background())
	wlog.Info(ctx, "server started", "port", 8080)
}
