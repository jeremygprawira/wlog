// Command detach shows wlog.Detach for work that outlives the request, such as
// sending an email after a checkout already answered the client.
package main

import (
	"context"
	"fmt"

	"github.com/jeremygprawira/wlog"
)

// sendEmail starts its own event under the parent's context, so the email keeps its
// own trace link without waiting for, or merging into, the checkout event.
func sendEmail(ctx context.Context) {
	ctx, end := wlog.Detach(ctx, "email.send")
	defer end()
	wlog.Set(ctx, "email.recipient_id", "u-42")
}

func main() {
	log := wlog.New(wlog.WithService("detach-example", "0.0.1", "local"))
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "checkout")
	sendEmail(ctx)
	end()

	fmt.Println("checkout done")
}
