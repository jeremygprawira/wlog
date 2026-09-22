// This file drives a real client against the fake server that ships with the library, so the
// ack rule of Receive is proven without an outside service.
package wlogpubsub

import (
	"context"
	"testing"

	"cloud.google.com/go/pubsub/v2"
	pb "cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/v2/pstest"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestPubsub_AckRule proves that a successful handler acks the message and a failed one nacks
// it, so the service delivers it again. The fake server ships inside the pubsub module.
func TestPubsub_AckRule(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := pstest.NewServer()
	t.Cleanup(func() { _ = server.Close() })
	conn, err := grpc.NewClient(server.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial the fake server: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client, err := pubsub.NewClient(ctx, "project", option.WithGRPCConn(conn))
	if err != nil {
		t.Fatalf("build the client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	topic := "projects/project/topics/orders"
	if _, err := client.TopicAdminClient.CreateTopic(ctx, &pb.Topic{Name: topic}); err != nil {
		t.Fatalf("create the topic: %v", err)
	}
	if _, err := client.SubscriptionAdminClient.CreateSubscription(ctx, &pb.Subscription{
		Name: "projects/project/subscriptions/orders-sub", Topic: topic,
	}); err != nil {
		t.Fatalf("create the subscription: %v", err)
	}
	server.Publish(topic, []byte("payload"), nil)

	log, rec := wlogtest.New(t)
	attempts := 0
	_ = Receive(ctx, log, client.Subscriber("orders-sub"), func(ctx context.Context, msg *pubsub.Message) error {
		attempts++
		if attempts == 1 {
			// The first delivery fails, so Receive nacks the message and the service
			// delivers it again.
			return errString("boom")
		}
		cancel()
		return nil
	})

	if attempts < 2 {
		t.Fatalf("attempts = %d, want the nacked message delivered again", attempts)
	}
	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d, want one per delivery", len(events))
	}
	if events[0]["level"] != "error" {
		t.Errorf("the first delivery recorded level %v, want error", events[0]["level"])
	}
	if events[1]["level"] != "info" {
		t.Errorf("the second delivery recorded level %v, want info", events[1]["level"])
	}
}
