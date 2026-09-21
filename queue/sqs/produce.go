// This file holds the producer side: one call per send or publish, and the trace attribute of
// the context.
package wlogsqs

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// SNSClient is the part of *sns.Client that Publish uses. *sns.Client satisfies it, and a test
// fake does too.
type SNSClient interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
}

// SendMessage sends one message through client, records one call on the event of ctx, and adds
// the trace attributes of ctx to the message.
func SendMessage(ctx context.Context, client SQSClient, params *sqs.SendMessageInput) (*sqs.SendMessageOutput, error) {
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "aws_sqs", Operation: "publish", Target: queueNameOf(params.QueueUrl),
	})
	params.MessageAttributes = withTraceAttributes(ctx, params.MessageAttributes)
	out, err := client.SendMessage(ctx, params)
	end(resultOf(err))
	return out, err
}

// Publish publishes one message through client, records one call on the event of ctx, and adds
// the trace attributes of ctx to the message.
func Publish(ctx context.Context, client SNSClient, params *sns.PublishInput) (*sns.PublishOutput, error) {
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "aws_sns", Operation: "publish", Target: topicNameOf(params.TopicArn),
	})
	params.MessageAttributes = withSNSTraceAttributes(ctx, params.MessageAttributes)
	out, err := client.Publish(ctx, params)
	end(resultOf(err))
	return out, err
}

// traceValues returns the trace headers of ctx as a plain map, and nil when ctx carries no
// trace.
func traceValues(ctx context.Context) map[string]string {
	if _, ok := propagate.FromContext(ctx); !ok {
		return nil
	}
	values := propagate.MapCarrier{}
	propagate.Inject(ctx, values)
	return values
}

// withTraceAttributes returns the attributes of one SQS message with the trace headers of ctx
// added. A context with no trace context keeps the attributes as they were.
func withTraceAttributes(ctx context.Context, attributes map[string]sqstypes.MessageAttributeValue) map[string]sqstypes.MessageAttributeValue {
	values := traceValues(ctx)
	if len(values) == 0 {
		return attributes
	}
	if attributes == nil {
		attributes = map[string]sqstypes.MessageAttributeValue{}
	}
	for key, value := range values {
		attributes[key] = sqstypes.MessageAttributeValue{
			DataType: aws.String("String"), StringValue: aws.String(value),
		}
	}
	return attributes
}

// withSNSTraceAttributes returns the attributes of one SNS message with the trace headers of
// ctx added. A context with no trace context keeps the attributes as they were.
func withSNSTraceAttributes(ctx context.Context, attributes map[string]snstypes.MessageAttributeValue) map[string]snstypes.MessageAttributeValue {
	values := traceValues(ctx)
	if len(values) == 0 {
		return attributes
	}
	if attributes == nil {
		attributes = map[string]snstypes.MessageAttributeValue{}
	}
	for key, value := range values {
		attributes[key] = snstypes.MessageAttributeValue{
			DataType: aws.String("String"), StringValue: aws.String(value),
		}
	}
	return attributes
}

// topicNameOf returns the name of a topic from its ARN, which is the last colon separated part.
func topicNameOf(arn *string) string {
	if arn == nil {
		return ""
	}
	return (*arn)[strings.LastIndexByte(*arn, ':')+1:]
}

// resultOf builds the call result of one finished publish.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
