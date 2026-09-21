// This file runs the work conformance suite against the invocation path, and checks the
// fields, the cold start rule, and the flush rule of Wrap.
package wloglambda

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestLambda_C1_WorkConformance proves that the invocation path passes every scenario of the
// work suite.
func TestLambda_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the event path Wrap uses. The suite supplies the unit,
// because one invocation carries no rpc, message, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestLambda_C1_InvocationRecordsIdsAndRemaining proves that one invocation records the
// invocation id, the function name, the cold start flag, and the remaining time.
func TestLambda_C1_InvocationRecordsIdsAndRemaining(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := Wrap(log, func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })

	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID:       "req-1",
		InvokedFunctionArn: "arn:aws:lambda:us-east-1:123456789012:function:orders",
	})
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(3*time.Second))
	defer cancel()

	if _, err := h(ctx, struct{}{}); err != nil {
		t.Fatalf("the handler returned %v", err)
	}
	got := lastEvent(t, rec)
	if got["kind"] != "function" || got["operation"] != "function orders" {
		t.Errorf("kind/operation = %v/%v, want function/function orders", got["kind"], got["operation"])
	}
	faas, _ := got["faas"].(map[string]any)
	for key, want := range map[string]any{
		"system": "aws_lambda", "name": "orders", "invocation_id": "req-1", "cold_start": true,
	} {
		if !conformance.Equal(faas[key], want) {
			t.Errorf("faas.%s = %v, want %v", key, faas[key], want)
		}
	}
	if remaining, ok := faas["remaining_ms"].(int64); !ok || remaining < 2900 || remaining > 3100 {
		t.Errorf("faas.remaining_ms = %v, want about 3000", faas["remaining_ms"])
	}
}

// TestLambda_C1_ColdStartIsTheFirstInvocation proves that exactly one event of many
// concurrent invocations carries cold_start true.
func TestLambda_C1_ColdStartIsTheFirstInvocation(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := Wrap(log, func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = h(context.Background(), struct{}{})
		}()
	}
	wg.Wait()

	cold := 0
	for _, event := range rec.Events() {
		faas, _ := event["faas"].(map[string]any)
		if faas["cold_start"] == true {
			cold++
		}
	}
	if cold != 1 {
		t.Errorf("cold_start is true in %d events, want 1", cold)
	}
}

// TestLambda_C1_APIRequestFillsHTTPAndStatus proves that an API Gateway v1 event fills the
// http group, and a server error response gives level error.
func TestLambda_C1_APIRequestFillsHTTPAndStatus(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := Wrap(log, func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		return events.APIGatewayProxyResponse{StatusCode: 503}, nil
	})

	in := events.APIGatewayProxyRequest{
		HTTPMethod: "GET", Path: "/orders/42", Resource: "/orders/{id}",
		RequestContext: events.APIGatewayProxyRequestContext{Protocol: "HTTP/1.1"},
	}
	if _, err := h(context.Background(), in); err != nil {
		t.Fatalf("the handler returned %v", err)
	}
	got := lastEvent(t, rec)
	http, _ := got["http"].(map[string]any)
	for key, want := range map[string]any{
		"method": "GET", "path": "/orders/42", "route": "/orders/{id}", "protocol": "HTTP/1.1",
	} {
		if http[key] != want {
			t.Errorf("http.%s = %v, want %v", key, http[key], want)
		}
	}
	if !conformance.Equal(http["status"], 503) {
		t.Errorf("http.status = %v, want 503", http["status"])
	}
	if got["level"] != "error" {
		t.Errorf("level = %v, want error for a 503", got["level"])
	}
	faas, _ := got["faas"].(map[string]any)
	if faas["trigger"] != "http" {
		t.Errorf("faas.trigger = %v, want http", faas["trigger"])
	}
}

// TestLambda_C1_BatchEventNamesTriggerAndSize proves that an SQS event names its trigger and
// its batch size.
func TestLambda_C1_BatchEventNamesTriggerAndSize(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := Wrap(log, func(context.Context, events.SQSEvent) (string, error) { return "ok", nil })

	_, _ = h(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "1"}, {MessageId: "2"}}})

	faas, _ := lastEvent(t, rec)["faas"].(map[string]any)
	if faas["trigger"] != "sqs" || !conformance.Equal(faas["batch_size"], 2) {
		t.Errorf("faas = %v, want trigger sqs and batch_size 2", faas)
	}
}

// TestLambda_C1_XRayHeaderJoinsTheTrace proves that the X-Ray header of the invocation
// context sets the trace id of the event.
func TestLambda_C1_XRayHeaderJoinsTheTrace(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := Wrap(log, func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })

	ctx := context.WithValue(context.Background(), "x-amzn-trace-id", "Root=1-5759e988-bd862e3fe1be46a994272793;Parent=53995c3f42cd8ad8;Sampled=1") //nolint:staticcheck // the runtime stores the header under this string key
	if _, err := h(ctx, struct{}{}); err != nil {
		t.Fatalf("the handler returned %v", err)
	}

	trace, _ := lastEvent(t, rec)["trace"].(map[string]any)
	if trace["trace_id"] != "5759e988bd862e3fe1be46a994272793" {
		t.Errorf("trace.trace_id = %v, want the trace id of the X-Ray header", trace["trace_id"])
	}
}

// TestLambda_C7_PipelineDeliversEveryEvent proves that a handler behind a pipeline delivers
// every event across three warm invocations, and that a panicking invocation delivers its
// event and panics again.
func TestLambda_C7_PipelineDeliversEveryEvent(t *testing.T) {
	sender := &fakeSender{}
	log := wlog.New(
		wlog.WithSilent(),
		wlog.WithService("lambda-test", "0.0.1", "local"),
		wlog.WithDrains(pipeline.Wrap(sender)),
	)
	h := Wrap(log, func(context.Context, map[string]any) (string, error) { return "ok", nil })

	for i := 0; i < 3; i++ {
		if _, err := h(context.Background(), map[string]any{"i": i}); err != nil {
			t.Fatalf("invocation %d returned %v", i, err)
		}
	}

	panicked := false
	func() {
		defer func() {
			panicked = recover() != nil
		}()
		_, _ = Wrap(log, func(context.Context, map[string]any) (string, error) { panic("boom") })(context.Background(), nil)
	}()
	if !panicked {
		t.Error("the panicking invocation did not panic again")
	}

	if count := sender.count(); count != 4 {
		t.Errorf("delivered %d events, want 4 across three warm invocations and a panic", count)
	}
}

// lastEvent returns the only recorded event, and stops the test when the run recorded none.
func lastEvent(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	return got
}
