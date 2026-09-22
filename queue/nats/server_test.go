// This file drives a real NATS server inside the test process, so the drain and its factory
// are proven against the wire. The fake publisher of the other tests shows neither the flush
// of a batch nor the close of the connection.
package wlognats

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/jeremygprawira/wlog/setup"
)

// TestNats_FactoryDrainReachesTheServer proves that the factory builds a drain from the two
// variables, and that Close sends every pending event to a real server.
func TestNats_FactoryDrainReachesTheServer(t *testing.T) {
	url := startNATSServer(t)

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect the subscriber: %v", err)
	}
	t.Cleanup(conn.Close)
	sub, err := conn.SubscribeSync("events")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatalf("flush the subscription: %v", err)
	}

	drain, err := Factory().New(setup.MapEnv{
		"WLOG_NATS_URL":     url,
		"WLOG_NATS_SUBJECT": "events",
	})
	if err != nil {
		t.Fatalf("build the drain: %v", err)
	}

	drain.Send(context.Background(), map[string]any{"event_id": "e1"})
	drain.Send(context.Background(), map[string]any{"event_id": "e2"})
	closer, ok := drain.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("the drain has no Close")
	}
	if err := closer.Close(context.Background()); err != nil {
		t.Fatalf("close the drain: %v", err)
	}

	for _, want := range []string{"e1", "e2"} {
		msg, err := sub.NextMsg(2 * time.Second)
		if err != nil {
			t.Fatalf("the server delivered no message for %s: %v", want, err)
		}
		var event map[string]any
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			t.Fatalf("the payload is not JSON: %v", err)
		}
		if event["event_id"] != want {
			t.Errorf("event_id = %v, want %s", event["event_id"], want)
		}
	}
}

// startNATSServer starts an embedded NATS server on a free port, and returns its URL.
func startNATSServer(t *testing.T) string {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{
		Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	go server.Start()
	t.Cleanup(server.Shutdown)
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("the server did not start")
	}
	return server.ClientURL()
}
