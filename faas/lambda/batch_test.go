// This file runs the batch helpers: one event per record, and the failure list of a batch.
package wloglambda

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLambda_C8_ProcessSQSReturnsTheFailingRecord proves that one failed record of three
// lands in BatchItemFailures, and every record gets its own event.
func TestLambda_C8_ProcessSQSReturnsTheFailingRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	batch := events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "a"}, {MessageId: "b"}, {MessageId: "c"},
	}}

	out := ProcessSQS(context.Background(), log, batch, func(_ context.Context, record events.SQSMessage) error {
		if record.MessageId == "b" {
			return errString("boom")
		}
		return nil
	})

	if len(out.BatchItemFailures) != 1 || out.BatchItemFailures[0].ItemIdentifier != "b" {
		t.Errorf("BatchItemFailures = %v, want the id of the failed record", out.BatchItemFailures)
	}
	if count := len(rec.Events()); count != 3 {
		t.Errorf("events = %d, want one per record", count)
	}
}

// TestLambda_C1_BatchFailuresLandOnTheInvocation proves that the failed record count lands on
// the open invocation event.
func TestLambda_BatchFailuresLandOnTheInvocation(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "function handler")

	_ = ProcessSQS(ctx, log, events.SQSEvent{Records: []events.SQSMessage{{MessageId: "a"}}},
		func(context.Context, events.SQSMessage) error { return errString("boom") })
	end()

	faas, _ := rec.Last()["faas"].(map[string]any)
	if !conformance.Equal(faas["batch_failures"], 1) {
		t.Errorf("faas.batch_failures = %v, want 1", faas["batch_failures"])
	}
}

// TestLambda_C1_SQSRecordRecordsDeliveryCountAndLag proves that one SQS record records its
// queue, its id, its delivery count, and the time it waited.
func TestLambda_SQSRecordRecordsDeliveryCountAndLag(t *testing.T) {
	log, rec := wlogtest.New(t)
	sent := time.Now().Add(-2 * time.Second)
	batch := events.SQSEvent{Records: []events.SQSMessage{{
		MessageId:      "msg-1",
		EventSourceARN: "arn:aws:sqs:us-east-1:123456789012:orders",
		Attributes: map[string]string{
			"ApproximateReceiveCount": "3",
			"SentTimestamp":           strconv.FormatInt(sent.UnixMilli(), 10),
		},
	}}}

	_ = ProcessSQS(context.Background(), log, batch, func(context.Context, events.SQSMessage) error { return nil })

	got := lastEvent(t, rec)
	if got["operation"] != "process orders" {
		t.Errorf("operation = %v, want process orders", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "aws_sqs", "destination": "orders", "message_id": "msg-1", "delivery_count": 3,
	} {
		if !conformance.Equal(messaging[key], want) {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
}

// TestLambda_C1_ProcessKinesisReturnsTheFailingRecord proves that one failed Kinesis record
// lands in BatchItemFailures.
func TestLambda_ProcessKinesisReturnsTheFailingRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	batch := events.KinesisEvent{Records: []events.KinesisEventRecord{
		{EventID: "e1", EventSourceArn: "arn:aws:kinesis:us-east-1:123456789012:stream/orders",
			Kinesis: events.KinesisRecord{SequenceNumber: "seq-1"}},
		{EventID: "e2", EventSourceArn: "arn:aws:kinesis:us-east-1:123456789012:stream/orders",
			Kinesis: events.KinesisRecord{SequenceNumber: "seq-2"}},
	}}

	out := ProcessKinesis(context.Background(), log, batch, func(_ context.Context, record events.KinesisEventRecord) error {
		if record.EventID == "e2" {
			return errString("boom")
		}
		return nil
	})

	if len(out.BatchItemFailures) != 1 || out.BatchItemFailures[0].ItemIdentifier != "seq-2" {
		t.Errorf("BatchItemFailures = %v, want the sequence number of the failed record", out.BatchItemFailures)
	}
	messaging, _ := lastEvent(t, rec)["messaging"].(map[string]any)
	if messaging["system"] != "aws_kinesis" || messaging["destination"] != "orders" {
		t.Errorf("messaging = %v, want system aws_kinesis and destination orders", messaging)
	}
}

// TestLambda_C1_ProcessDynamoDBReturnsTheFailingRecord proves that one failed DynamoDB
// record lands in BatchItemFailures, and the item images never reach the event.
func TestLambda_ProcessDynamoDBReturnsTheFailingRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	batch := events.DynamoDBEvent{Records: []events.DynamoDBEventRecord{
		{EventID: "e1", EventSourceArn: "arn:aws:dynamodb:us-east-1:123456789012:table/orders/stream/2026",
			Change: events.DynamoDBStreamRecord{
				SequenceNumber: "seq-1",
				NewImage:       map[string]events.DynamoDBAttributeValue{"secret": events.NewStringAttribute("hunter2")},
			}},
	}}

	out := ProcessDynamoDB(context.Background(), log, batch, func(context.Context, events.DynamoDBEventRecord) error {
		return errString("boom")
	})

	if len(out.BatchItemFailures) != 1 || out.BatchItemFailures[0].ItemIdentifier != "seq-1" {
		t.Errorf("BatchItemFailures = %v, want the sequence number of the failed record", out.BatchItemFailures)
	}
	got := lastEvent(t, rec)
	messaging, _ := got["messaging"].(map[string]any)
	if messaging["system"] != "aws_dynamodb" || messaging["destination"] != "orders" {
		t.Errorf("messaging = %v, want system aws_dynamodb and destination orders", messaging)
	}
	if body, err := json.Marshal(got); err != nil {
		t.Fatalf("marshal the event: %v", err)
	} else if strings.Contains(string(body), "hunter2") {
		t.Errorf("the item image reached the event: %s", body)
	}
}

// errString is the plain error a test returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
