// This file runs the work conformance suite against the consumer path, and checks the receive
// attributes, the delete rule, and criterion 4.
package wlogsqs

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestSqs_C1_WorkConformance proves that the consumer event path passes every scenario of the
// work suite.
func TestSqs_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path Receive uses. The suite supplies the unit, because
// an SQS message carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestSqs_C4_ThirdReceiveRecordsDeliveryCount proves that a message received for the third time
// records delivery_count 3, its queue name, its id, its lag, and its trace.
func TestSqs_C4_ThirdReceiveRecordsDeliveryCount(t *testing.T) {
	client := &fakeSQSClient{
		messages: []sqstypes.Message{message(3, time.Now().Add(-2*time.Second))},
		readErr:  io.EOF,
	}
	log, rec := wlogtest.New(t)

	err := Receive(context.Background(), log, client, receiveInput("orders"), func(context.Context, sqstypes.Message) error { return nil })
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Receive returned %v, want the read error that ended the loop", err)
	}
	if deleted := client.deleted(); len(deleted) != 1 {
		t.Fatalf("deletes = %d, want 1 after a successful handler", len(deleted))
	}

	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	if got["operation"] != "process orders" {
		t.Errorf("operation = %v, want process orders", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "aws_sqs", "operation": "process", "destination": "orders",
		"message_id": "msg-1", "delivery_count": 3,
	} {
		if !conformance.Equal(messaging[key], want) {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the attribute", trace["trace_id"])
	}
}

// TestSqs_C1_ReceiveAsksForAttributes proves that every receive asks for the receive count and
// the send time, so the event can carry them.
func TestSqs_C1_ReceiveAsksForAttributes(t *testing.T) {
	client := &fakeSQSClient{readErr: io.EOF}
	log, _ := wlogtest.New(t)

	_ = Receive(context.Background(), log, client, receiveInput("orders"), func(context.Context, sqstypes.Message) error { return nil })

	asked := map[sqstypes.MessageSystemAttributeName]bool{}
	for _, name := range client.lastReceive().MessageSystemAttributeNames {
		asked[name] = true
	}
	for _, name := range []sqstypes.MessageSystemAttributeName{
		sqstypes.MessageSystemAttributeNameApproximateReceiveCount,
		sqstypes.MessageSystemAttributeNameSentTimestamp,
	} {
		if !asked[name] {
			t.Errorf("the receive does not ask for %s", name)
		}
	}
}

// TestSqs_C1_FailedMessageIsNotDeleted proves that a failed handler leaves the message for its
// visibility timeout, and the loop continues.
func TestSqs_C1_FailedMessageIsNotDeleted(t *testing.T) {
	client := &fakeSQSClient{
		messages: []sqstypes.Message{message(1, time.Now())},
		readErr:  io.EOF,
	}
	log, rec := wlogtest.New(t)

	err := Receive(context.Background(), log, client, receiveInput("orders"), func(context.Context, sqstypes.Message) error {
		return errString("handler failed")
	})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Receive returned %v, want the read error that ended the loop", err)
	}
	if deleted := client.deleted(); len(deleted) != 0 {
		t.Errorf("deletes = %d, want none after a failed handler", len(deleted))
	}
	if level := rec.Last()["level"]; level != "error" {
		t.Errorf("level = %v, want error", level)
	}
}

// message builds one received message with a receive count, a send time, and a trace attribute.
func message(count int, sent time.Time) sqstypes.Message {
	return sqstypes.Message{
		MessageId:     aws.String("msg-1"),
		ReceiptHandle: aws.String("receipt-1"),
		Attributes: map[string]string{
			"ApproximateReceiveCount": strconv.Itoa(count),
			"SentTimestamp":           strconv.FormatInt(sent.UnixMilli(), 10),
		},
		MessageAttributes: map[string]sqstypes.MessageAttributeValue{
			"traceparent": {
				DataType:    aws.String("String"),
				StringValue: aws.String("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
			},
		},
	}
}

// receiveInput builds the input of one queue.
func receiveInput(queue string) *sqs.ReceiveMessageInput {
	return &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String("https://sqs.us-east-1.amazonaws.com/123456789012/" + queue),
		MaxNumberOfMessages: 10,
	}
}

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
