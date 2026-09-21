// Command lambda-orders is a Lambda function with wlog around every invocation. It is the
// lambda recipe's example: one event per invocation, a flush before the runtime freezes the
// process, and the trace of the caller.
package main

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/jeremygprawira/wlog"
	wloglambda "github.com/jeremygprawira/wlog/faas/lambda"
)

// handle is the work of one invocation. The handler adds the field a searcher asks for.
func handle(ctx context.Context, in events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	wlog.Set(ctx, "order_id", in.PathParameters["id"])
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       `{"id":"` + in.PathParameters["id"] + `"}`,
	}, nil
}

func main() {
	logger := wlog.New(wlog.WithService("lambda-orders", "0.0.1", "prod"))
	lambda.StartWithOptions(
		wloglambda.Wrap(logger, handle),
		wloglambda.SIGTERMFlush(logger),
	)
}
