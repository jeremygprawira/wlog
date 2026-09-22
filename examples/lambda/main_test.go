// This file invokes one API Gateway request and compares the event with the recipe's
// hand-written golden. The schema tool validates the golden against schema/event.v1.json.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"

	wloglambda "github.com/jeremygprawira/wlog/faas/lambda"
	"github.com/jeremygprawira/wlog/internal/conformance"
)

// TestLambda_GoldenEvent proves that one API Gateway request gives the event the recipe
// documents.
func TestLambda_GoldenEvent(t *testing.T) {
	rec := conformance.NewMemoryRecorder()
	wrapped := wloglambda.Wrap(rec.Logger(), handle)

	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID:       "recipe-1",
		InvokedFunctionArn: "arn:aws:lambda:us-east-1:123456789012:function:lambda-orders",
	})
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(5*time.Second))
	defer cancel()

	request := events.APIGatewayProxyRequest{
		HTTPMethod: "GET", Path: "/orders/42", Resource: "/orders/{id}",
		PathParameters: map[string]string{"id": "42"},
		RequestContext: events.APIGatewayProxyRequestContext{Protocol: "HTTP/1.1", RequestID: "recipe-1"},
	}
	response, err := wrapped(ctx, request)
	if err != nil {
		t.Fatalf("the handler returned %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", response.StatusCode)
	}

	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(golden(t)), got); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}

	// The normalized compare drops the trace ids, so the shape is checked here.
	trace, _ := events[0]["trace"].(map[string]any)
	if trace["span_id"] == "" || trace["span_id"] == trace["parent_span_id"] {
		t.Errorf("trace = %v, want a span id of its own", trace)
	}
}

// golden reads the recipe's hand-written event.
func golden(t *testing.T) map[string]any {
	t.Helper()
	body, err := os.ReadFile("testdata/event.json")
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	event := map[string]any{}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("parse the golden: %v", err)
	}
	return event
}
