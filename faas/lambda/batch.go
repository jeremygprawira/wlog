// This file holds the batch helpers: one message event per record, and the failure list
// that tells the event source which records to retry.
package wloglambda

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// ProcessSQS gives every record of one SQS batch its own message event, and returns the ids
// of the records whose handler failed, so the event source retries only those records. The
// event source mapping must set ReportBatchItemFailures, or AWS ignores the list and retries
// the whole batch. A nil Logger means wlog.Default.
func ProcessSQS(ctx context.Context, log *wlog.Logger, e events.SQSEvent, fn func(context.Context, events.SQSMessage) error) events.SQSEventResponse {
	var out events.SQSEventResponse
	for _, record := range e.Records {
		if err := process(ctx, log, sqsUnit(record), func(ctx context.Context) error {
			return fn(ctx, record)
		}); err != nil {
			out.BatchItemFailures = append(out.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return out
}

// ProcessKinesis gives every record of one Kinesis batch its own message event, and returns
// the ids of the records whose handler failed. A nil Logger means wlog.Default.
func ProcessKinesis(ctx context.Context, log *wlog.Logger, e events.KinesisEvent, fn func(context.Context, events.KinesisEventRecord) error) events.KinesisEventResponse {
	var out events.KinesisEventResponse
	for _, record := range e.Records {
		if err := process(ctx, log, kinesisUnit(record), func(ctx context.Context) error {
			return fn(ctx, record)
		}); err != nil {
			out.BatchItemFailures = append(out.BatchItemFailures, events.KinesisBatchItemFailure{ItemIdentifier: record.Kinesis.SequenceNumber})
		}
	}
	return out
}

// ProcessDynamoDB gives every record of one DynamoDB stream batch its own message event, and
// returns the ids of the records whose handler failed. It never reads the item images,
// because an item holds user data. A nil Logger means wlog.Default.
func ProcessDynamoDB(ctx context.Context, log *wlog.Logger, e events.DynamoDBEvent, fn func(context.Context, events.DynamoDBEventRecord) error) events.DynamoDBEventResponse {
	var out events.DynamoDBEventResponse
	for _, record := range e.Records {
		if err := process(ctx, log, dynamoUnit(record), func(ctx context.Context) error {
			return fn(ctx, record)
		}); err != nil {
			out.BatchItemFailures = append(out.BatchItemFailures, events.DynamoDBBatchItemFailure{ItemIdentifier: record.Change.SequenceNumber})
		}
	}
	return out
}

// sqsUnit maps one SQS record onto a unit of work. The receive count and the send time come
// from the record attributes, so the event carries delivery_count and lag_ms.
func sqsUnit(record events.SQSMessage) work.Unit {
	fields := map[string]any{"system": "aws_sqs", "operation": "process"}
	if name := queueNameOf(record.EventSourceARN); name != "" {
		fields["destination"] = name
	}
	if record.MessageId != "" {
		fields["message_id"] = record.MessageId
	}
	if count, err := strconv.Atoi(record.Attributes["ApproximateReceiveCount"]); err == nil {
		fields["delivery_count"] = count
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   sqsCarrier(record.MessageAttributes),
		StartedAt: sentTimeOf(record),
	}
}

// kinesisUnit maps one Kinesis record onto a unit of work. The arrival time becomes the
// start time, so the event carries the time the record waited as lag_ms.
func kinesisUnit(record events.KinesisEventRecord) work.Unit {
	fields := map[string]any{"system": "aws_kinesis", "operation": "process"}
	if name := nameOf(record.EventSourceArn, "stream/"); name != "" {
		fields["destination"] = name
	}
	if record.EventID != "" {
		fields["message_id"] = record.EventID
	}
	if record.Kinesis.SequenceNumber != "" {
		fields["kinesis"] = map[string]any{"sequence_number": record.Kinesis.SequenceNumber}
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		StartedAt: record.Kinesis.ApproximateArrivalTimestamp.Time,
	}
}

// dynamoUnit maps one DynamoDB stream record onto a unit of work. It reads no item image,
// because an item holds user data.
func dynamoUnit(record events.DynamoDBEventRecord) work.Unit {
	fields := map[string]any{"system": "aws_dynamodb", "operation": "process"}
	if name := nameOf(record.EventSourceArn, "table/"); name != "" {
		fields["destination"] = name
	}
	if record.EventID != "" {
		fields["message_id"] = record.EventID
	}
	if record.Change.SequenceNumber != "" {
		fields["dynamodb"] = map[string]any{"sequence_number": record.Change.SequenceNumber}
	}
	return work.Unit{Kind: work.KindMessage, Fields: fields}
}

// queueNameOf returns the name of a queue from its ARN, which is the last colon separated
// part.
func queueNameOf(arn string) string {
	if arn == "" {
		return ""
	}
	return arn[strings.LastIndexByte(arn, ':')+1:]
}

// nameOf returns the name that follows one marker in an ARN, such as "stream/" or "table/".
func nameOf(arn, marker string) string {
	at := strings.Index(arn, marker)
	if at < 0 {
		return ""
	}
	name := arn[at+len(marker):]
	if end := strings.IndexByte(name, '/'); end >= 0 {
		return name[:end]
	}
	return name
}

// sentTimeOf returns the send time of one SQS record, and the zero time when the record
// carries none.
func sentTimeOf(record events.SQSMessage) time.Time {
	millis, err := strconv.ParseInt(record.Attributes["SentTimestamp"], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(millis)
}

// sqsCarrier wraps the string attributes of one SQS record as a propagate carrier, so a
// traceparent attribute joins the trace of the producer.
func sqsCarrier(attributes map[string]events.SQSMessageAttribute) propagate.Carrier {
	values := make(map[string]string, len(attributes))
	for key, value := range attributes {
		if value.StringValue != nil {
			values[key] = *value.StringValue
		}
	}
	if len(values) == 0 {
		return nil
	}
	return propagate.MapCarrier(values)
}
