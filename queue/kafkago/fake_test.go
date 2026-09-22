// This file holds the broker fake that the producer and the drain tests share. It answers
// the metadata and produce requests of a kafka.Writer, so no test needs a real broker.
package wlogkafkago

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/segmentio/kafka-go"
	metadatapi "github.com/segmentio/kafka-go/protocol/metadata"
	produceapi "github.com/segmentio/kafka-go/protocol/produce"
)

// fakeBroker answers the requests of one kafka.Writer. It records every message it
// received, so a test can read the headers and the bodies the writer sent.
type fakeBroker struct {
	mu      sync.Mutex
	batches [][]kafka.Message
	fail    error
	hold    chan struct{}
}

// RoundTrip answers one request of the writer.
func (b *fakeBroker) RoundTrip(_ context.Context, _ net.Addr, req kafka.Request) (kafka.Response, error) {
	switch typed := req.(type) {
	case *metadatapi.Request:
		return b.metadata(typed), nil
	case *produceapi.Request:
		return b.produce(typed)
	}
	return nil, fmt.Errorf("fakeBroker: unexpected request %T", req)
}

// metadata answers one metadata request with one partition on one broker.
func (b *fakeBroker) metadata(req *metadatapi.Request) *metadatapi.Response {
	topics := make([]metadatapi.ResponseTopic, 0, len(req.TopicNames))
	for _, name := range req.TopicNames {
		topics = append(topics, metadatapi.ResponseTopic{
			Name:       name,
			Partitions: []metadatapi.ResponsePartition{{PartitionIndex: 0, LeaderID: 1}},
		})
	}
	return &metadatapi.Response{
		Brokers: []metadatapi.ResponseBroker{{NodeID: 1, Host: "broker", Port: 9092}},
		Topics:  topics,
	}
}

// produce answers one produce request, and records the messages it carried. A held broker
// waits for the test to close the hold channel, so a test can see a write in flight.
func (b *fakeBroker) produce(req *produceapi.Request) (*produceapi.Response, error) {
	if b.hold != nil {
		<-b.hold
	}
	b.mu.Lock()
	failure := b.fail
	b.mu.Unlock()
	if failure != nil {
		return nil, failure
	}

	topic := req.Topics[0]
	partition := topic.Partitions[0]
	var received []kafka.Message
	for {
		record, err := partition.RecordSet.Records.ReadRecord()
		if err != nil {
			break
		}
		msg := kafka.Message{Headers: append([]kafka.Header(nil), record.Headers...)}
		if record.Value != nil {
			msg.Value, _ = io.ReadAll(record.Value)
		}
		received = append(received, msg)
	}
	b.mu.Lock()
	b.batches = append(b.batches, received)
	b.mu.Unlock()

	return &produceapi.Response{Topics: []produceapi.ResponseTopic{{
		Topic:      topic.Topic,
		Partitions: []produceapi.ResponsePartition{{Partition: partition.Partition}},
	}}}, nil
}

// failWith makes every later produce fail with err.
func (b *fakeBroker) failWith(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fail = err
}

// messages returns every message the broker received, oldest first.
func (b *fakeBroker) messages() []kafka.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []kafka.Message
	for _, batch := range b.batches {
		out = append(out, batch...)
	}
	return out
}

// bodies returns the value of every message, joined, which is what a drain put on the wire.
func (b *fakeBroker) bodies() []byte {
	var out []byte
	for _, msg := range b.messages() {
		out = append(out, msg.Value...)
	}
	return out
}
