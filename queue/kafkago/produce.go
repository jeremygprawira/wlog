// This file holds the producer side: the wrapper around *kafka.Writer that records one
// call per write and adds the trace headers of the current unit of work to each message.
package wlogkafka

import (
	"context"
	"sync"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Producer wraps a *kafka.Writer so every write records one call on the event of the
// context and adds the trace headers of that context to each message.
//
// Writer owns the Completion function of the wrapped writer, because that is where an
// async call ends. Build one wrapper per writer.
type Producer struct {
	w *kafka.Writer
}

// Writer returns the wrapper around w. Build it once at startup, and call its
// WriteMessages in place of the writer's own.
func Writer(w *kafka.Writer) *Producer {
	p := &Producer{w: w}
	w.Completion = p.complete
	return p
}

// WriteMessages writes msgs and records one call on the event of ctx.
func (p *Producer) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	return p.write(ctx, callOf(p.w, msgs), msgs)
}

// write writes msgs and records the call. Writer derives the call from the writer and the
// messages.
func (p *Producer) write(ctx context.Context, call wlog.Call, msgs []kafka.Message) error {
	ctx, end := wlog.StartCall(ctx, call)
	finished := &callEnd{end: end}
	msgs = prepare(ctx, finished, msgs)

	err := p.w.WriteMessages(ctx, msgs...)
	if err != nil {
		// An error before a batch runs, such as a message over the batch limit, never
		// reaches Completion.
		finished.finish(err)
	}
	return err
}

// complete ends the call of every message in one finished batch. kafka-go calls it once per
// batch in both the sync and the async mode, and a batch may mix the messages of two
// writes, so every call ends once.
func (p *Producer) complete(msgs []kafka.Message, err error) {
	ended := map[*callEnd]bool{}
	for i := range msgs {
		finished, ok := msgs[i].WriterData.(*callEnd)
		if !ok || ended[finished] {
			continue
		}
		ended[finished] = true
		finished.finish(err)
	}
}

// prepare returns a copy of msgs with the trace headers of ctx and the call end of this
// write, so the Completion function can end the right call. The messages of the caller stay
// untouched.
func prepare(ctx context.Context, finished *callEnd, msgs []kafka.Message) []kafka.Message {
	out := make([]kafka.Message, len(msgs))
	for i, msg := range msgs {
		out[i] = msg
		out[i].WriterData = finished
		out[i].Headers = withTraceHeaders(ctx, msg.Headers)
	}
	return out
}

// withTraceHeaders returns a copy of the headers of one message with the trace context of
// ctx added, so the next service joins the same trace. A context with no trace context
// keeps the headers exactly as they were, and a repeated header key keeps every value.
func withTraceHeaders(ctx context.Context, headers []kafka.Header) []kafka.Header {
	if _, ok := propagate.FromContext(ctx); !ok {
		return headers
	}
	carrier := propagate.NewBytesCarrier(nil)
	propagate.Inject(ctx, carrier)
	out := make([]kafka.Header, len(headers), len(headers)+3)
	copy(out, headers)
	for _, key := range carrier.Keys() {
		out = setHeader(out, key, carrier.Get(key))
	}
	return out
}

// setHeader returns headers with key set to value. It replaces the first header of that
// key and appends one when the key is absent, so every other header keeps its place.
func setHeader(headers []kafka.Header, key, value string) []kafka.Header {
	for i := range headers {
		if headers[i].Key == key {
			headers[i].Value = []byte(value)
			return headers
		}
	}
	return append(headers, kafka.Header{Key: key, Value: []byte(value)})
}

// callEnd ends one call once, when the broker reports the result of its batch.
type callEnd struct {
	end  func(wlog.CallResult)
	once sync.Once
}

// finish records the result of the batch. A second result adds nothing, so a call that
// WriteMessages already ended keeps the first result.
func (c *callEnd) finish(err error) {
	c.once.Do(func() { c.end(resultOf(err)) })
}

// resultOf builds the call result of one finished write.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}

// callOf names the call of one write: a queue publish to the topic of the messages.
func callOf(w *kafka.Writer, msgs []kafka.Message) wlog.Call {
	return wlog.Call{Kind: "queue", System: "kafka", Operation: "publish", Target: topicOf(w, msgs)}
}

// topicOf returns the topic of one write: the topic of the first message that names one,
// and the topic of the writer when no message names one.
func topicOf(w *kafka.Writer, msgs []kafka.Message) string {
	for _, msg := range msgs {
		if msg.Topic != "" {
			return msg.Topic
		}
	}
	return w.Topic
}
