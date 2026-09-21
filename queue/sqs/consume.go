// This file holds the consumer side: the receive loop, the attributes it asks for, and the
// mapping from one message onto one unit of work.
package wlogsqs

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// SQSClient is the part of *sqs.Client that this adapter uses. *sqs.Client satisfies it, and a
// test fake does too, because no test can call AWS.
type SQSClient interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

// Handler handles one received message inside its event.
type Handler func(ctx context.Context, msg sqstypes.Message) error

// Receive reads messages with client in a loop until the context ends or a receive fails. Each
// message gets one event, and Receive deletes the message after the handler returns nil. A
// failed message is left for its visibility timeout, so the queue delivers it again.
//
// Receive asks for the receive count and the send time of every message, so the event carries
// delivery_count and lag_ms. A nil Logger means wlog.Default.
func Receive(ctx context.Context, log *wlog.Logger, client SQSClient, input *sqs.ReceiveMessageInput, fn Handler) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		out, err := client.ReceiveMessage(ctx, withAttributes(input))
		if err != nil {
			return err
		}
		for _, msg := range out.Messages {
			err := process(ctx, log, unitOf(msg, input.QueueUrl), func(ctx context.Context) error {
				return fn(ctx, msg)
			})
			if err != nil {
				continue
			}
			if _, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl: input.QueueUrl, ReceiptHandle: msg.ReceiptHandle,
			}); err != nil {
				return err
			}
		}
	}
}

// withAttributes returns a copy of input that asks for the two system attributes the event
// needs, next to the names the caller asked for.
func withAttributes(input *sqs.ReceiveMessageInput) *sqs.ReceiveMessageInput {
	out := *input
	names := []sqstypes.MessageSystemAttributeName{
		sqstypes.MessageSystemAttributeNameApproximateReceiveCount,
		sqstypes.MessageSystemAttributeNameSentTimestamp,
	}
	for _, name := range input.MessageSystemAttributeNames {
		if name != names[0] && name != names[1] {
			names = append(names, name)
		}
	}
	out.MessageSystemAttributeNames = names
	return &out
}

// process runs one unit of work through the event path of this adapter: one event, the group
// of the kind, and a recovered panic as an error.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// unitOf maps one received message onto a unit of work. The send time becomes the start time,
// so the event carries the time the message waited as lag_ms.
func unitOf(msg sqstypes.Message, queueURL *string) work.Unit {
	fields := map[string]any{
		"system":    "aws_sqs",
		"operation": "process",
	}
	if name := queueNameOf(queueURL); name != "" {
		fields["destination"] = name
	}
	if msg.MessageId != nil {
		fields["message_id"] = *msg.MessageId
	}
	if count, err := strconv.Atoi(receiveCountOf(msg)); err == nil {
		fields["delivery_count"] = count
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   carrierOf(msg.MessageAttributes),
		StartedAt: sentTimeOf(msg),
	}
}

// receiveCountOf returns the ApproximateReceiveCount of one message, and an empty string when
// the message carries none.
func receiveCountOf(msg sqstypes.Message) string {
	return msg.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateReceiveCount)]
}

// sentTimeOf returns the send time of one message, and the zero time when the message carries
// none.
func sentTimeOf(msg sqstypes.Message) time.Time {
	millis, err := strconv.ParseInt(msg.Attributes[string(sqstypes.MessageSystemAttributeNameSentTimestamp)], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(millis)
}

// queueNameOf returns the name of a queue from its URL, which is the last path segment.
func queueNameOf(queueURL *string) string {
	if queueURL == nil {
		return ""
	}
	return (*queueURL)[strings.LastIndexByte(*queueURL, '/')+1:]
}

// carrierOf wraps the string attributes of one message as a propagate carrier, so a
// traceparent attribute joins the trace of the producer.
func carrierOf(attributes map[string]sqstypes.MessageAttributeValue) propagate.Carrier {
	values := make(map[string]string, len(attributes))
	for key, value := range attributes {
		if value.StringValue != nil {
			values[key] = *value.StringValue
		}
	}
	return propagate.MapCarrier(values)
}
