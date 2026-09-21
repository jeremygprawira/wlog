// This file holds the producer side: the sync and async wrappers that add the trace headers
// of the context and record one call per send. A wrapper exists because
// sarama.ProducerInterceptor has no context, so it cannot read the trace of the caller.
package wlogsarama

import (
	"context"
	"errors"
	"sync"

	"github.com/IBM/sarama"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// SyncSender is the part of sarama.SyncProducer that Sync uses. sarama.SyncProducer
// satisfies it, and so does a fake, which keeps this adapter working when sarama adds a
// method to its own interface.
type SyncSender interface {
	SendMessage(msg *sarama.ProducerMessage) (int32, int64, error)
	SendMessages(msgs []*sarama.ProducerMessage) error
	Close() error
}

// AsyncSender is the part of sarama.AsyncProducer that Async uses. sarama.AsyncProducer
// satisfies it, and so does a fake.
type AsyncSender interface {
	Input() chan<- *sarama.ProducerMessage
	Successes() <-chan *sarama.ProducerMessage
	Errors() <-chan *sarama.ProducerError
	AsyncClose()
	Close() error
}

// The real interfaces satisfy the contracts of this adapter, so a sarama change that drops
// one method fails the build and not a user.
var (
	_ SyncSender  = (sarama.SyncProducer)(nil)
	_ AsyncSender = (sarama.AsyncProducer)(nil)
)

// Sync wraps a sync sender so every send records one call on the event of the context and
// adds the trace headers of that context. The send methods take the context, because the
// sarama interface carries none.
type Sync struct {
	p SyncSender
}

// SyncProducer returns the wrapper around p.
func SyncProducer(p SyncSender) *Sync { return &Sync{p: p} }

// SendMessage produces msg, records one call, and returns the partition, the offset, and the
// error of the wrapped producer. The message carries the trace headers after the send.
func (s *Sync) SendMessage(ctx context.Context, msg *sarama.ProducerMessage) (int32, int64, error) {
	ctx, end := wlog.StartCall(ctx, callOf(msg))
	msg.Headers = withTraceHeaders(ctx, msg.Headers)
	partition, offset, err := s.p.SendMessage(msg)
	end(resultOf(err))
	return partition, offset, err
}

// SendMessages produces msgs as one call.
func (s *Sync) SendMessages(ctx context.Context, msgs []*sarama.ProducerMessage) error {
	var first *sarama.ProducerMessage
	if len(msgs) > 0 {
		first = msgs[0]
	}
	ctx, end := wlog.StartCall(ctx, callOf(first))
	for _, msg := range msgs {
		msg.Headers = withTraceHeaders(ctx, msg.Headers)
	}
	err := s.p.SendMessages(msgs)
	end(resultOf(err))
	return err
}

// Close closes the wrapped producer.
func (s *Sync) Close() error { return s.p.Close() }

// Async wraps an async sender so every queued message carries the trace headers of the
// context and records one call. The call ends when the producer reports the message on
// Successes or Errors, so read those channels from the wrapper.
type Async struct {
	p       AsyncSender
	mu      sync.Mutex
	pending map[*sarama.ProducerMessage]func(wlog.CallResult)
	success chan *sarama.ProducerMessage
	fail    chan *sarama.ProducerError
}

// AsyncProducer returns the wrapper around p and starts the goroutine that ends the calls of
// produced messages. The config must turn on Producer.Return.Successes, because a call ends
// when the broker reports the message, and a producer that reports nothing leaves every call
// open. sarama already requires Producer.Return.Errors.
func AsyncProducer(p AsyncSender, cfg *sarama.Config) (*Async, error) {
	if cfg == nil || !cfg.Producer.Return.Successes {
		return nil, errors.New("wlogsarama: set Config.Producer.Return.Successes, or a produced call never ends")
	}
	a := &Async{
		p:       p,
		pending: map[*sarama.ProducerMessage]func(wlog.CallResult){},
		success: make(chan *sarama.ProducerMessage),
		fail:    make(chan *sarama.ProducerError),
	}
	go a.watch()
	return a, nil
}

// Send queues msg for the wrapped producer, adds the trace headers of ctx to msg, and
// records one call that ends when the producer reports the message. The message keeps its
// identity, so the result channels report the same pointer.
func (a *Async) Send(ctx context.Context, msg *sarama.ProducerMessage) {
	ctx, end := wlog.StartCall(ctx, callOf(msg))
	msg.Headers = withTraceHeaders(ctx, msg.Headers)
	a.mu.Lock()
	a.pending[msg] = end
	a.mu.Unlock()
	a.p.Input() <- msg
}

// Successes returns the channel of produced messages. Read it, because the wrapped producer
// blocks while a result channel is full.
func (a *Async) Successes() <-chan *sarama.ProducerMessage { return a.success }

// Errors returns the channel of failed messages.
func (a *Async) Errors() <-chan *sarama.ProducerError { return a.fail }

// AsyncClose asks the wrapped producer to stop after it delivers the queued messages.
func (a *Async) AsyncClose() { a.p.AsyncClose() }

// Close stops the wrapped producer after it delivers the queued messages.
func (a *Async) Close() error { return a.p.Close() }

// watch reads the result channels of the wrapped producer, ends the call of every reported
// message, and forwards the result, until both channels close.
func (a *Async) watch() {
	successes := a.p.Successes()
	failures := a.p.Errors()
	for successes != nil || failures != nil {
		select {
		case msg, ok := <-successes:
			if !ok {
				successes = nil
				continue
			}
			a.finish(msg, nil)
			a.success <- msg
		case failure, ok := <-failures:
			if !ok {
				failures = nil
				continue
			}
			a.finish(failure.Msg, failure.Err)
			a.fail <- failure
		}
	}
	close(a.success)
	close(a.fail)
}

// finish ends the call of one reported message, when it started one.
func (a *Async) finish(msg *sarama.ProducerMessage, err error) {
	a.mu.Lock()
	end := a.pending[msg]
	delete(a.pending, msg)
	a.mu.Unlock()
	if end != nil {
		end(resultOf(err))
	}
}

// withTraceHeaders returns a copy of the headers of one message with the trace context of
// ctx added, so the next service joins the same trace. A context with no trace context
// keeps the headers exactly as they were, and a repeated header key keeps every value.
func withTraceHeaders(ctx context.Context, headers []sarama.RecordHeader) []sarama.RecordHeader {
	if _, ok := propagate.FromContext(ctx); !ok {
		return headers
	}
	carrier := propagate.NewBytesCarrier(nil)
	propagate.Inject(ctx, carrier)
	out := make([]sarama.RecordHeader, len(headers), len(headers)+3)
	copy(out, headers)
	for _, key := range carrier.Keys() {
		out = setHeader(out, key, carrier.Get(key))
	}
	return out
}

// setHeader returns headers with key set to value. It replaces the first header of that key
// and appends one when the key is absent, so every other header keeps its place.
func setHeader(headers []sarama.RecordHeader, key, value string) []sarama.RecordHeader {
	for i := range headers {
		if string(headers[i].Key) == key {
			headers[i].Value = []byte(value)
			return headers
		}
	}
	return append(headers, sarama.RecordHeader{Key: []byte(key), Value: []byte(value)})
}

// callOf names the call of one send: a queue publish to the topic of the message.
func callOf(msg *sarama.ProducerMessage) wlog.Call {
	target := ""
	if msg != nil {
		target = msg.Topic
	}
	return wlog.Call{Kind: "queue", System: "kafka", Operation: "publish", Target: target}
}

// resultOf builds the call result of one finished send.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
