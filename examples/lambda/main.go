// Command lambda runs the example handler on AWS Lambda.
package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/jeremygprawira/wlog"
)

func main() {
	logger := wlog.New(wlog.WithService("lambda-example", "0.0.1", envName()))
	lambda.Start(Handler(logger, handle))
}

// handle is the example's own work. The wrapper adds the event and the flush.
func handle(ctx context.Context, in map[string]any) (any, error) {
	wlog.Set(ctx, "input_keys", len(in))
	return map[string]any{"ok": true}, nil
}

// envName reads WLOG_ENV, defaulting to local.
func envName() string {
	if value := os.Getenv("WLOG_ENV"); value != "" {
		return value
	}
	return "local"
}
