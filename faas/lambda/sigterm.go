// This file holds the spindown flush.
package wloglambda

import (
	"context"
	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/jeremygprawira/wlog"
)

// sigtermFlushTimeout is the budget of the flush that runs on SIGTERM. The runtime kills
// the process about 500 milliseconds after the signal.
const sigtermFlushTimeout = 400 * time.Millisecond

// SIGTERMFlush returns a lambda.Option that flushes the drains when the runtime sends
// SIGTERM, which is the last moment before the runtime kills the process. Pass it to
// lambda.Start next to the other options.
func SIGTERMFlush(log *wlog.Logger) lambda.Option {
	return lambda.WithEnableSIGTERM(func() {
		flush(log, context.Background(), sigtermFlushTimeout)
	})
}
