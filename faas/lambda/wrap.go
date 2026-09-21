// This file holds the invocation wrapper, the cold start flag, the known event types, and
// the trace of one invocation.
package wloglambda

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// defaultFlushTimeout is the budget of the flush that runs after every invocation.
const defaultFlushTimeout = 2 * time.Second

// Option configures Wrap.
type Option func(*config)

// config holds the resolved options of one wrapper.
type config struct {
	flushTimeout time.Duration
}

// WithFlushTimeout sets the budget of the flush that runs after every invocation, and after
// a panic. A Lambda with a short timeout needs a smaller budget than the default of 2
// seconds.
func WithFlushTimeout(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.flushTimeout = d
		}
	}
}

// Wrap returns a handler that gives every invocation one function event. It records the
// invocation id, the function name and version, the cold start flag, and the remaining
// time, and it fills the http group and the status for API Gateway v1, API Gateway v2, and
// ALB events. It flushes the drains before it returns, and it flushes before a panic
// continues, because a Lambda freezes the process between invocations. A nil Logger means
// wlog.Default.
func Wrap[TIn, TOut any](log *wlog.Logger, h func(context.Context, TIn) (TOut, error), opts ...Option) func(context.Context, TIn) (TOut, error) {
	cfg := config{flushTimeout: defaultFlushTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	var cold atomic.Bool
	return func(ctx context.Context, in TIn) (TOut, error) {
		var out TOut
		defer flush(log, cfg.flushTimeout)

		err := work.Run(ctx, log, unitOf(ctx, in, cold.CompareAndSwap(false, true)), func(ctx context.Context) error {
			setHTTPRequest(ctx, in)
			var handlerErr error
			out, handlerErr = h(ctx, in)
			recordResponse(ctx, out)
			return handlerErr
		})
		return out, err
	}
}

// flush sends the pending events of log on its own deadline, because the invocation context
// is often spent when the handler returns.
func flush(log *wlog.Logger, timeout time.Duration) {
	if log == nil {
		log = wlog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = log.Flush(ctx)
}

// unitOf maps one invocation onto a unit of work. The remaining time comes from the
// invocation deadline, and the cold start flag from the first call in this process.
func unitOf(ctx context.Context, in any, coldStart bool) work.Unit {
	fields := map[string]any{"system": "aws_lambda", "cold_start": coldStart}
	if lambdacontext.FunctionName != "" {
		fields["name"] = lambdacontext.FunctionName
	}
	if lambdacontext.FunctionVersion != "" {
		fields["version"] = lambdacontext.FunctionVersion
	}
	if lambdaCtx, ok := lambdacontext.FromContext(ctx); ok {
		if lambdaCtx.AwsRequestID != "" {
			fields["invocation_id"] = lambdaCtx.AwsRequestID
		}
		if _, present := fields["name"]; !present {
			if name := functionName(lambdaCtx.InvokedFunctionArn); name != "" {
				fields["name"] = name
			}
		}
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline).Milliseconds(); remaining > 0 {
			fields["remaining_ms"] = remaining
		}
	}
	triggerOf(in, fields)
	return work.Unit{Kind: work.KindFunction, Fields: fields, Carrier: carrierOf(ctx)}
}

// triggerOf writes the trigger and the batch size of a known event type. Every other event
// type stays unnamed.
func triggerOf(in any, fields map[string]any) {
	switch event := in.(type) {
	case events.APIGatewayProxyRequest, events.APIGatewayV2HTTPRequest, events.ALBTargetGroupRequest:
		fields["trigger"] = "http"
	case events.SQSEvent:
		fields["trigger"] = "sqs"
		fields["batch_size"] = len(event.Records)
	case events.SNSEvent:
		fields["trigger"] = "sns"
		fields["batch_size"] = len(event.Records)
	case events.KinesisEvent:
		fields["trigger"] = "kinesis"
		fields["batch_size"] = len(event.Records)
	case events.DynamoDBEvent:
		fields["trigger"] = "dynamodb"
		fields["batch_size"] = len(event.Records)
	case events.EventBridgeEvent:
		fields["trigger"] = "eventbridge"
		fields["batch_size"] = 1
	}
}

// setHTTPRequest fills the http group of an API Gateway or ALB event, so an invocation that
// serves a request carries the same http fields as an HTTP adapter.
func setHTTPRequest(ctx context.Context, in any) {
	var method, path, route, protocol string
	switch event := in.(type) {
	case events.APIGatewayProxyRequest:
		method, path, protocol = event.HTTPMethod, event.Path, event.RequestContext.Protocol
		route = event.Resource
	case events.APIGatewayV2HTTPRequest:
		method, path, protocol = event.RequestContext.HTTP.Method, event.RawPath, event.RequestContext.HTTP.Protocol
		route = event.RouteKey
	case events.ALBTargetGroupRequest:
		method, path = event.HTTPMethod, event.Path
	default:
		return
	}
	fields := []any{}
	if method != "" {
		fields = append(fields, "method", method)
	}
	if path != "" {
		fields = append(fields, "path", path)
	}
	if route != "" {
		fields = append(fields, "route", route)
	}
	if protocol != "" {
		fields = append(fields, "protocol", protocol)
	}
	if len(fields) > 0 {
		wlog.SetGroup(ctx, "http", fields...)
	}
}

// recordResponse writes the status of a known response type into the http group, and the
// level that status asks for.
func recordResponse(ctx context.Context, out any) {
	status := 0
	switch response := out.(type) {
	case events.APIGatewayProxyResponse:
		status = response.StatusCode
	case events.APIGatewayV2HTTPResponse:
		status = response.StatusCode
	case events.ALBTargetGroupResponse:
		status = response.StatusCode
	}
	if status == 0 {
		return
	}
	wlog.SetGroup(ctx, "http", "status", status)
	switch work.ClassOf(work.KindRequest, strconv.Itoa(status)) {
	case work.StatusClientError:
		wlog.SetLevel(ctx, wlog.LevelWarn)
	case work.StatusServerError:
		wlog.SetLevel(ctx, wlog.LevelError)
	}
}

// carrierOf returns the trace of one invocation as a W3C carrier. The Lambda runtime puts
// the X-Ray header on the context under the string key "x-amzn-trace-id", and the work kit
// reads W3C headers, so this function converts the header through propagate.
func carrierOf(ctx context.Context) propagate.Carrier {
	value, _ := ctx.Value("x-amzn-trace-id").(string) //nolint:staticcheck // the runtime stores the header under this string key
	if value == "" {
		return nil
	}
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{"X-Amzn-Trace-Id": {value}}, propagate.WithXRay())
	carrier := propagate.HeaderCarrier{}
	propagate.Inject(ctx, carrier)
	return carrier
}

// functionName reads the last segment of a function ARN.
func functionName(arn string) string {
	if index := strings.LastIndexByte(arn, ':'); index >= 0 {
		return arn[index+1:]
	}
	return arn
}
