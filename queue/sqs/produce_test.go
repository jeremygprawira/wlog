// This file runs the calls conformance suite against the producer path, and checks the call
// record and the trace attribute that only this adapter has.
package wlogsqs

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestSqs_C1_CallsConformance proves that the producer path passes every scenario of the calls
// suite.
func TestSqs_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real trace attribute. The suite
// names the call and its result, because an SQS send is not an http, db, or cache call. The
// record of SendMessage and Publish has its own tests below.
type callsFactory struct{}

// Call records one publish call and writes the trace attribute of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	withTraceAttributes(ctx, nil)
	end(result)
	return result.Err
}

// TestSqs_C1_SendMessageCallRecord proves that one send records one queue call and writes a
// traceparent attribute whose span id is the span id of that call.
func TestSqs_C1_SendMessageCallRecord(t *testing.T) {
	client := &fakeSQSClient{}
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)

	out, err := SendMessage(ctx, client, &sqs.SendMessageInput{
		QueueUrl:    aws.String("https://sqs.us-east-1.amazonaws.com/123456789012/orders"),
		MessageBody: aws.String("payload"),
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if out.MessageId == nil {
		t.Error("SendMessage returned no message id")
	}
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "aws_sqs", "operation": "publish", "target": "orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	checkTraceSpan(t, attributeText(client.lastSend().MessageAttributes), record["span_id"])
}

// TestSqs_C1_PublishCallRecord proves that one SNS publish records one queue call and writes a
// traceparent attribute.
func TestSqs_C1_PublishCallRecord(t *testing.T) {
	client := &fakeSNSClient{}
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)

	if _, err := Publish(ctx, client, &sns.PublishInput{
		TopicArn: aws.String("arn:aws:sns:us-east-1:123456789012:orders"),
		Message:  aws.String("payload"),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "aws_sns", "operation": "publish", "target": "orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	attributes := map[string]string{}
	for key, value := range client.last().MessageAttributes {
		if value.StringValue != nil {
			attributes[key] = *value.StringValue
		}
	}
	checkTraceSpan(t, attributes, record["span_id"])
}

// TestSqs_C1_SendMessageError proves that a send the client refuses records the error and hands
// it back.
func TestSqs_C1_SendMessageError(t *testing.T) {
	client := &fakeSQSClient{sendErr: errString("send refused")}
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)

	_, err := SendMessage(ctx, client, &sqs.SendMessageInput{QueueUrl: aws.String("https://example.com/orders")})
	if err == nil || err.Error() != "send refused" {
		t.Fatalf("SendMessage returned %v, want the error of the client", err)
	}
	end()

	if record := firstCall(t, rec.Last()); record["error"] == nil {
		t.Error("calls[0].error = nil, want the error of the client")
	}
}

// tracedContext starts one event with a trace, and returns its context and the end func.
func tracedContext(t *testing.T, log *wlog.Logger) (context.Context, func()) {
	t.Helper()
	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	return wlog.Start(ctx, "op")
}

// attributeText returns the text of the string attributes of one SQS message.
func attributeText(attributes map[string]sqstypes.MessageAttributeValue) map[string]string {
	out := map[string]string{}
	for key, value := range attributes {
		if value.StringValue != nil {
			out[key] = *value.StringValue
		}
	}
	return out
}

// checkTraceSpan proves that the traceparent of the attributes names the given span id.
func checkTraceSpan(t *testing.T, attributes map[string]string, spanID any) {
	t.Helper()
	parts := strings.Split(attributes["traceparent"], "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %q, want the trace id of the unit", attributes["traceparent"])
	}
	if parts[2] != spanID {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], spanID)
	}
}

// firstCall returns the first call record of one event.
func firstCall(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	if event == nil {
		t.Fatal("no event recorded")
	}
	calls, _ := event["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	return record
}
