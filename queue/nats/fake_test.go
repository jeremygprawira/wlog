// This file holds the fakes of the tests: a publisher that records the messages, and a
// JetStream message that records the acknowledgement it received.
package wlognats

import (
	"context"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// fakePublisher records every published message, and reports err when the test set one.
type fakePublisher struct {
	mu       sync.Mutex
	messages []*nats.Msg
	err      error
	flushes  int
}

// FlushWithContext records one flush.
func (p *fakePublisher) FlushWithContext(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushes++
	return nil
}

// flushed returns how many flushes the drain asked for.
func (p *fakePublisher) flushed() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.flushes
}

// PublishMsg records one message.
func (p *fakePublisher) PublishMsg(msg *nats.Msg) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, msg)
	return nil
}

// last returns the last published message, and nil when the publisher sent none.
func (p *fakePublisher) last() *nats.Msg {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.messages) == 0 {
		return nil
	}
	return p.messages[len(p.messages)-1]
}

// bodies returns the payloads of every message, joined, which is what a drain put on the wire.
func (p *fakePublisher) bodies() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []byte
	for _, msg := range p.messages {
		out = append(out, msg.Data...)
	}
	return out
}

// fakeJetStreamMsg is one JetStream message that records the acknowledgement it received.
type fakeJetStreamMsg struct {
	subject     string
	header      nats.Header
	metadata    *jetstream.MsgMetadata
	metadataErr error
	ack         string
}

// Metadata returns the metadata of the message.
func (m *fakeJetStreamMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return m.metadata, m.metadataErr
}

// Data returns the payload of the message.
func (*fakeJetStreamMsg) Data() []byte { return nil }

// Headers returns the headers of the message.
func (m *fakeJetStreamMsg) Headers() nats.Header { return m.header }

// Subject returns the subject of the message.
func (m *fakeJetStreamMsg) Subject() string { return m.subject }

// Reply returns the reply subject of the message.
func (*fakeJetStreamMsg) Reply() string { return "" }

// Ack records a positive acknowledgement.
func (m *fakeJetStreamMsg) Ack() error {
	m.ack = "ack"
	return nil
}

// DoubleAck records nothing, because the tests do not use it.
func (*fakeJetStreamMsg) DoubleAck(context.Context) error { return nil }

// Nak records a negative acknowledgement.
func (m *fakeJetStreamMsg) Nak() error {
	m.ack = "nak"
	return nil
}

// NakWithDelay records nothing, because the tests do not use it.
func (*fakeJetStreamMsg) NakWithDelay(time.Duration) error { return nil }

// InProgress records nothing, because the tests do not use it.
func (*fakeJetStreamMsg) InProgress() error { return nil }

// Term records a term.
func (m *fakeJetStreamMsg) Term() error {
	m.ack = "term"
	return nil
}

// TermWithReason records nothing, because the tests do not use it.
func (*fakeJetStreamMsg) TermWithReason(string) error { return nil }
