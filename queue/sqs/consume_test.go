// This file runs the work conformance suite against the consumer path, and checks the receive
// attributes, the delete rule, and criterion 4.
package wlogsqs

import (
	"context"
	"errors"
	"fmt"
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

// workFactory drives the real Receive path with a fake client, so a change that breaks the
// adapter fails the suite.
type workFactory struct{}

// Declare names the one kind an SQS consumer produces. SQS reports a redelivery count.
func (workFactory) Declare() workconformance.Declaration {
	return workconformance.Declaration{
		Kinds: []work.Kind{work.KindMessage}, System: "aws_sqs", DeliveryCount: true,
	}
}

// Process runs one unit of work through Receive. The suite expects no panic from Process, so
// the panic of the handler, which Receive raises again, comes back as an error.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	destination, _ := unit.Fields["destination"].(string)
	msg := message(1, unit.StartedAt)
	if count, ok := unit.Fields["delivery_count"].(int); ok {
		msg.Attributes["ApproximateReceiveCount"] = strconv.Itoa(count)
	}
	client := &fakeSQSClient{messages: []sqstypes.Message{msg}, readErr: io.EOF}
	var handlerErr error
	_ = Receive(context.Background(), log, client, receiveInput(destination), func(ctx context.Context, _ sqstypes.Message) error {
		handlerErr = handler(ctx)
		return handlerErr
	})
	// The loop reports the read error that ended it. The suite asks for the result of the
	// handler, so the factory reports that one.
	if handlerErr != nil {
		return handlerErr
	}
	return nil
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
func TestSqs_ReceiveAsksForAttributes(t *testing.T) {
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

	// SQS returns only the message attributes a receive names, so the trace attributes
	// must be asked for too.
	attributes := map[string]bool{}
	for _, name := range client.lastReceive().MessageAttributeNames {
		attributes[name] = true
	}
	for _, name := range []string{"traceparent", "tracestate", "X-Request-ID"} {
		if !attributes[name] {
			t.Errorf("the receive does not ask for the %s message attribute", name)
		}
	}
}

// TestSqs_C1_FailedMessageIsNotDeleted proves that a failed handler leaves the message for its
// visibility timeout, and the loop continues.
func TestSqs_FailedMessageIsNotDeleted(t *testing.T) {
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

// TestSqs_DeleteErrorKeepsTheBatch proves that a delete error leaves the rest of the batch
// running, and the caller sees the error once.
func TestSqs_DeleteErrorKeepsTheBatch(t *testing.T) {
	client := &fakeSQSClient{
		messages:  []sqstypes.Message{message(1, time.Now()), message(1, time.Now())},
		readErr:   io.EOF,
		deleteErr: errString("delete refused"),
	}
	log, rec := wlogtest.New(t)

	err := Receive(context.Background(), log, client, receiveInput("orders"), func(context.Context, sqstypes.Message) error { return nil })
	if err == nil || err.Error() != "delete refused" {
		t.Fatalf("Receive returned %v, want the delete error", err)
	}
	if count := len(rec.Events()); count != 2 {
		t.Errorf("events = %d, want one per message of the batch", count)
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
