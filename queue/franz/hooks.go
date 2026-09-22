// This file holds the client hooks: the two produce hooks that record one call per produced
// record and add the trace headers, and the fetch hook that reads the trace of a fetched
// record into its context.
package wlogfranz

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Hooks returns the value for kgo.WithHooks of this adapter. One value implements the two
// produce hooks and the fetch hook, so one registration covers both sides of a client.
//
// The produce hooks record one call per record on the event of the record context, and they
// add the trace headers of that context. The fetch hook reads the trace headers of a record
// into its context, so Record and the code after it join the trace of the producer.
func Hooks() kgo.Hook { return hooks{} }

// hooks implements the produce and the fetch hooks of franz-go.
type hooks struct{}

// OnProduceRecordBuffered starts one call for one buffered record and adds the trace headers
// of the record context. It runs on the goroutine that calls Produce, so the context of the
// caller is available.
//
// franz-go keeps the first context of a record, so a second produce of the same record carries
// the context of the first. The call of the first produce stays, and the record keeps its
// trace.
func (hooks) OnProduceRecordBuffered(r *kgo.Record) {
	ctx := contextOf(r)
	if _, open := wlog.CallFromContext(ctx); open {
		r.Headers = withTraceHeaders(ctx, r.Headers)
		return
	}
	ctx, end := wlog.StartCall(ctx, callOf(r))
	r.Context = context.WithValue(ctx, callEndKey{}, end)
	r.Headers = withTraceHeaders(ctx, r.Headers)
}

// OnProduceRecordUnbuffered ends the call of one record with the result the broker reported.
// It runs on the goroutine that reports the promise, which is not the caller.
func (hooks) OnProduceRecordUnbuffered(r *kgo.Record, err error) {
	end, ok := r.Context.Value(callEndKey{}).(func(wlog.CallResult))
	if !ok {
		return
	}
	end(resultOf(err))
}

// OnFetchRecordBuffered reads the trace headers of one fetched record into its context, so
// the event of Record joins the trace of the producer.
func (hooks) OnFetchRecordBuffered(r *kgo.Record) {
	r.Context = propagate.Extract(contextOf(r), carrierOf(r.Headers))
}

// contextOf returns the context of one record, and a background context when the record
// carries none. franz-go fills the context of a polled record, and a record that a caller
// built by hand can carry none.
func contextOf(r *kgo.Record) context.Context {
	if r.Context == nil {
		return context.Background()
	}
	return r.Context
}

// callEndKey is the context key that carries the end func of one produce call from the
// buffered hook to the unbuffered hook.
type callEndKey struct{}

// callOf names the call of one produce: a queue publish to the topic of the record.
func callOf(r *kgo.Record) wlog.Call {
	return wlog.Call{Kind: "queue", System: "kafka", Operation: "publish", Target: r.Topic}
}

// resultOf builds the call result of one finished produce.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}

// withTraceHeaders returns a copy of the headers of one record with the trace context of ctx
// added, so the next service joins the same trace. A context with no trace context keeps the
// headers exactly as they were, and a repeated header key keeps every value.
func withTraceHeaders(ctx context.Context, headers []kgo.RecordHeader) []kgo.RecordHeader {
	if _, ok := propagate.FromContext(ctx); !ok {
		return headers
	}
	carrier := propagate.NewBytesCarrier(nil)
	propagate.Inject(ctx, carrier)
	out := make([]kgo.RecordHeader, len(headers), len(headers)+3)
	copy(out, headers)
	for _, key := range carrier.Keys() {
		out = setHeader(out, key, carrier.Get(key))
	}
	return out
}

// setHeader returns headers with key set to value. It replaces the first header of that key
// and appends one when the key is absent, so every other header keeps its place.
func setHeader(headers []kgo.RecordHeader, key, value string) []kgo.RecordHeader {
	for i := range headers {
		if headers[i].Key == key {
			headers[i].Value = []byte(value)
			return headers
		}
	}
	return append(headers, kgo.RecordHeader{Key: key, Value: []byte(value)})
}
