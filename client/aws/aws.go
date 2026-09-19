// Package wlogaws records one call per AWS SDK operation, with the service, the
// operation, the region, the request id, and the attempt count.
//
// Read top to bottom: Append adds one Initialize middleware to the API options of a
// client. The middleware wraps the whole operation, so a call with three attempts records
// one call record with attempts 3. The fields come from the metadata the stack returns,
// and the error code comes from the API error, never from its message.
//
// This is the whole setup:
//
//	wlogaws.Append(&cfg.APIOptions)
//	client := s3.NewFromConfig(cfg)
package wlogaws

import (
	"context"
	"errors"
	"strconv"

	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/jeremygprawira/wlog"
)

// middlewareID names the wlog middleware, so a second Append cannot record twice.
const middlewareID = "wlog"

// Append adds the wlog middleware to one slice of API options. Call it once per client
// config, before the client is built.
func Append(options *[]func(*middleware.Stack) error) {
	if options == nil {
		return
	}
	*options = append(*options, install)
}

// install adds the middleware to one stack, once. The API options of a client may run
// more than once, and the guard keeps a second run silent.
func install(stack *middleware.Stack) error {
	if _, ok := stack.Initialize.Get(middlewareID); ok {
		return nil
	}
	return stack.Initialize.Add(middleware.InitializeMiddlewareFunc(middlewareID, record), middleware.After)
}

// record opens one call for one operation and ends it with the result of the whole
// operation, retries included.
func record(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind:      "rpc",
		System:    "aws",
		Operation: awsmiddleware.GetServiceID(ctx) + "." + awsmiddleware.GetOperationName(ctx),
	})
	out, md, err := next.HandleInitialize(ctx, in)
	end(resultOf(ctx, md, err))
	return out, md, err
}

// resultOf builds the call result of one finished operation.
func resultOf(ctx context.Context, md middleware.Metadata, err error) wlog.CallResult {
	attrs := map[string]any{"attempts": attemptsOf(md)}
	if region := awsmiddleware.GetRegion(ctx); region != "" {
		attrs["region"] = region
	}
	if id, ok := awsmiddleware.GetRequestIDMetadata(md); ok && id != "" {
		attrs["request_id"] = id
	}
	result := wlog.CallResult{Status: "ok", Attrs: attrs}
	if status := statusOf(md, err); status != 0 {
		result.Status = strconv.Itoa(status)
	}
	if err != nil {
		result.Err = err
		result.ErrCode = errorCode(err)
	}
	return result
}

// attemptsOf returns the number of attempts of one operation, and one when the stack
// reports none.
func attemptsOf(md middleware.Metadata) int {
	if results, ok := retry.GetAttemptResults(md); ok && len(results.Results) > 0 {
		return len(results.Results)
	}
	return 1
}

// statusOf returns the HTTP status of one finished operation, and zero when the stack
// reports none.
func statusOf(md middleware.Metadata, err error) int {
	if err != nil {
		var responseErr *smithyhttp.ResponseError
		if errors.As(err, &responseErr) {
			return responseErr.HTTPStatusCode()
		}
		return 0
	}
	if response, ok := awsmiddleware.GetRawResponse(md).(*smithyhttp.Response); ok && response != nil {
		return response.StatusCode
	}
	return 0
}

// errorCode returns the code of one API error, such as AccessDenied, and it never holds
// the error message.
func errorCode(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if code := apiErr.ErrorCode(); code != "" {
			return code
		}
	}
	return "error"
}
