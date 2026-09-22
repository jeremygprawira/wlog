// This file holds the producer side: the wrapper around *kafka.Writer that records one
// call per write and adds the trace headers of the current unit of work to each message.
package wlogkafkago

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
// Writer owns the Completion function of the wrapped writer, because that is where a call
// ends. A Completion the caller already set runs after this one. Build one wrapper per writer.
type Producer struct {
	w        *kafka.Writer
	previous func([]kafka.Message, error)
}

// Writer returns the wrapper around w. Build it once at startup, and call its
// WriteMessages in place of the writer's own.
func Writer(w *kafka.Writer) *Producer {
	p := &Producer{w: w, previous: w.Completion}
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
	finished.expect(len(msgs))
	msgs = prepare(ctx, finished, msgs)

	err := p.w.WriteMessages(ctx, msgs...)
	if err != nil {
		// An error before a batch runs, such as a message over the batch limit, never
		// reaches Completion.
		finished.fail(err)
	}
	return err
}

// complete records the result of one finished batch, and calls a Completion the caller set.
// kafka-go calls it once per partition batch, and a batch may mix the messages of two writes,
// so every call ends once, after its last message reports.
func (p *Producer) complete(msgs []kafka.Message, err error) {
	counts := map[*callEnd]int{}
	for i := range msgs {
		data, ok := msgs[i].WriterData.(*writeData)
		if !ok {
			continue
		}
		counts[data.end]++
		// Restore the value of the caller, so a caller Completion sees its own data.
		msgs[i].WriterData = data.previous
	}
	for finished, count := range counts {
		finished.report(err, count)
	}
	if p.previous != nil {
		p.previous(msgs, err)
	}
}

// prepare returns a copy of msgs with the trace headers of ctx and the call end of this
// write, so the Completion function can end the right call. The messages of the caller stay
// untouched.
func prepare(ctx context.Context, finished *callEnd, msgs []kafka.Message) []kafka.Message {
	out := make([]kafka.Message, len(msgs))
	for i, msg := range msgs {
		out[i] = msg
		out[i].WriterData = &writeData{end: finished, previous: msg.WriterData}
		out[i].Headers = withTraceHeaders(ctx, msg.Headers)
	}
	return out
}

// writeData is the WriterData of one prepared message: the call end of its write, and the
// value the caller set.
type writeData struct {
	end      *callEnd
	previous any
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

// callEnd ends one call when the broker reports every message of its write. kafka-go reports
// one Completion per partition batch, so the call counts the messages and waits for the last
// report, and an error of any batch wins.
type callEnd struct {
	end     func(wlog.CallResult)
	mu      sync.Mutex
	pending int
	failed  error
	done    bool
}

// expect records how many messages the write sent.
func (c *callEnd) expect(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = n
}

// report records the result of one batch of n messages, and ends the call after the last
// message reports.
func (c *callEnd) report(err error, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	if err != nil && c.failed == nil {
		c.failed = err
	}
	c.pending -= n
	if c.pending > 0 {
		return
	}
	c.done = true
	c.end(resultOf(c.failed))
}

// fail ends the call of a write that failed before any batch reported.
func (c *callEnd) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	c.end(resultOf(err))
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
