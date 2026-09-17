---
name: instrument-with-wlog
description: Add one wide event to a Go handler or job, with the field naming rules.
---

<!-- wlog:skill -->

# Instrument with wlog

Open one event per unit of work, and add fields as the work runs.

1. Install the middleware once, at startup:
   `mux.Handle("/", wlogstd.Middleware(logger)(mux))`.
2. Add business context with `wlog.Set(ctx, "order_id", id)`.
3. Use `wlog.SetGroup(ctx, "payment", "method", "va", "amount", 150000)` for a group.
4. Report a failure with `wlog.Error(ctx, err)`, or `wlog.Errorf(ctx, "bad %s", name)`.
5. Use `wlog.Detach(ctx, "email.send")` for work that outlives the response.

Name a field in `snake_case`, under the owning group. Declare a `wlog.Key[T]` for a
field you set often, so a wrong value type fails to compile.

Never put a password, token, or card number on an event. The redactor masks a denied key,
but the field must not be written at all.

## A complete example

This program records one event. It compiles and runs as it stands:

```go
package main

import (
	"context"
	"fmt"

	"github.com/jeremygprawira/wlog"
)

func main() {
	logger := wlog.New(wlog.WithSilent())
	ctx, end := wlog.Start(logger.WithContext(context.Background()), "order.place")
	wlog.Set(ctx, "order_id", "ord-1")
	wlog.SetGroup(ctx, "payment", "method", "va", "amount", 150000)
	end()

	fmt.Println("one event recorded")
}
```
