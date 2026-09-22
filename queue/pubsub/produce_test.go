// This file runs the calls conformance suite against the producer path, and checks the attribute
// clone and the trace headers that only this adapter has.
package wlogpubsub

import (
	"context"
	"strings"
	"testing"

	"cloud.google.com/go/pubsub/v2"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestPubsub_C1_CallsConformance proves that the producer path passes every scenario of the calls
// suite.
func TestPubsub_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real trace attributes. The suite
// names the call and its result, because a publish is not an http, db, or cache call. The record
// of Publish has its own test below.
type callsFactory struct{}

// Call records one publish call and writes the trace attributes of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	withTraceAttributes(ctx, map[string]string{})
	end(result)
	return result.Err
}

// TestPubsub_C1_PublishAttributes proves that one publish adds the trace headers to a copy of the
// message attributes and leaves the attributes of the caller alone. The call itself ends when the
// service reports the result, which needs a live service and is an integration step.
func TestPubsub_PublishAttributes(t *testing.T) {
	pub := &fakePublisher{id: "orders"}
	log, _ := wlogtest.New(t)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := log.WithContext(base)
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	ctx, end := wlog.Start(ctx, "op")

	original := map[string]string{"x-tenant": "acme"}
	msg := &pubsub.Message{ID: "msg-1", Attributes: original}
	Publish(ctx, pub, msg)
	end()

	sent := pub.last()
	if sent == nil {
		t.Fatal("the publisher received no message")
	}
	if sent.Attributes["x-tenant"] != "acme" {
		t.Errorf("x-tenant = %q, want the attribute the caller set", sent.Attributes["x-tenant"])
	}
	parts := strings.Split(sent.Attributes["traceparent"], "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %q, want the trace id of the unit", sent.Attributes["traceparent"])
	}
	if len(original) != 1 || original["x-tenant"] != "acme" {
		t.Errorf("the attribute map of the caller changed: %v", original)
	}
}
