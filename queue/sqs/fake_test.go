// This file holds the fakes of the tests: a queue client that serves messages from a slice,
// and an SNS client that records the publishes. No test can call AWS.
package wlogsqs

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// fakeSQSClient serves messages from a slice, records every receive, delete, and send, and
// reports readErr when the slice ends.
type fakeSQSClient struct {
	mu       sync.Mutex
	messages []sqstypes.Message
	readErr  error
	sendErr  error
	receives []*sqs.ReceiveMessageInput
	deletes  []*sqs.DeleteMessageInput
	sends    []*sqs.SendMessageInput
}

// ReceiveMessage returns the messages of the slice, and readErr when the slice is empty. It
// keeps only the message attributes that the receive names, which is what SQS does.
func (c *fakeSQSClient) ReceiveMessage(_ context.Context, params *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.receives = append(c.receives, params)
	if len(c.messages) == 0 {
		return nil, c.readErr
	}
	asked := map[string]bool{}
	for _, name := range params.MessageAttributeNames {
		asked[name] = true
	}
	messages := make([]sqstypes.Message, 0, len(c.messages))
	for _, msg := range c.messages {
		kept := map[string]sqstypes.MessageAttributeValue{}
		for name, value := range msg.MessageAttributes {
			if asked[name] {
				kept[name] = value
			}
		}
		msg.MessageAttributes = kept
		messages = append(messages, msg)
	}
	c.messages = nil
	return &sqs.ReceiveMessageOutput{Messages: messages}, nil
}

// DeleteMessage records one delete.
func (c *fakeSQSClient) DeleteMessage(_ context.Context, params *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletes = append(c.deletes, params)
	return &sqs.DeleteMessageOutput{}, nil
}

// SendMessage records one send, and reports sendErr when the test set one.
func (c *fakeSQSClient) SendMessage(_ context.Context, params *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendErr != nil {
		return nil, c.sendErr
	}
	c.sends = append(c.sends, params)
	return &sqs.SendMessageOutput{MessageId: aws.String("sent-1")}, nil
}

// lastReceive returns the input of the last receive, and nil when the client received none.
func (c *fakeSQSClient) lastReceive() *sqs.ReceiveMessageInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.receives) == 0 {
		return nil
	}
	return c.receives[len(c.receives)-1]
}

// deleted returns the deleted messages.
func (c *fakeSQSClient) deleted() []*sqs.DeleteMessageInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*sqs.DeleteMessageInput(nil), c.deletes...)
}

// lastSend returns the input of the last send, and nil when the client sent none.
func (c *fakeSQSClient) lastSend() *sqs.SendMessageInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sends) == 0 {
		return nil
	}
	return c.sends[len(c.sends)-1]
}

// fakeSNSClient records every publish.
type fakeSNSClient struct {
	mu        sync.Mutex
	publishes []*sns.PublishInput
}

// Publish records one publish.
func (c *fakeSNSClient) Publish(_ context.Context, params *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishes = append(c.publishes, params)
	return &sns.PublishOutput{MessageId: aws.String("published-1")}, nil
}

// last returns the input of the last publish, and nil when the client published none.
func (c *fakeSNSClient) last() *sns.PublishInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.publishes) == 0 {
		return nil
	}
	return c.publishes[len(c.publishes)-1]
}
