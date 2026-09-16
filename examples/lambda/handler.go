// Package main is a runnable AWS Lambda example. Handler wraps a function with one wlog
// event per invocation and flushes the drains before it returns. A Lambda freezes
// between invocations, so an unflushed batch is lost.
package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/lambdacontext"

	"github.com/jeremygprawira/wlog"
)

// Handler wraps fn with one wide event per invocation. It sets faas.request_id,
// faas.name, faas.cold_start, and faas.remaining_ms, then flushes the drains on a
// deadline taken from the invocation's own remaining time.
func Handler(log *wlog.Logger, fn func(ctx context.Context, in map[string]any) (any, error)) func(ctx context.Context, in map[string]any) (any, error) {
	var state struct {
		mu     sync.Mutex
		warmed bool
	}
	coldStart := func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		first := !state.warmed
		state.warmed = true
		return first
	}

	return func(ctx context.Context, in map[string]any) (any, error) {
		cold := coldStart()
		ctx = log.WithContext(ctx)
		ctx, end := wlog.Start(ctx, "lambda.invoke")

		wlog.Set(ctx, "faas.cold_start", cold)
		if name := os.Getenv("AWS_LAMBDA_FUNCTION_NAME"); name != "" {
			wlog.Set(ctx, "faas.name", name)
		}
		if lambdaCtx, ok := lambdacontext.FromContext(ctx); ok {
			wlog.Set(ctx, "faas.request_id", lambdaCtx.AwsRequestID)
			if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") == "" {
				if name := functionName(lambdaCtx.InvokedFunctionArn); name != "" {
					wlog.Set(ctx, "faas.name", name)
				}
			}
		}
		if deadline, ok := ctx.Deadline(); ok {
			wlog.Set(ctx, "faas.remaining_ms", time.Until(deadline).Milliseconds())
		}

		out, err := fn(ctx, in)
		if err != nil {
			wlog.Error(ctx, err)
		}
		end()

		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = log.Close(closeCtx)
		return out, err
	}
}

// functionName reads the last segment of a function ARN.
func functionName(arn string) string {
	if index := strings.LastIndex(arn, ":"); index >= 0 {
		return arn[index+1:]
	}
	return arn
}
