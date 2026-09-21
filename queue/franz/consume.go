// This file holds the consumer side: the helper for a poll loop and the mapping from one
// fetched record onto one unit of work.
package wlogfranz

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/work"
)

// Record starts one event for one polled record, and returns the context of the handler and
// the end func. Commit the record after the end func reports success, so a failed record is
// delivered again. The consumer group comes from the client, which reports it as
// cl.OptValue(kgo.ConsumerGroup).
func Record(log *wlog.Logger, cl *kgo.Client, r *kgo.Record) (context.Context, func(error)) {
	group, _ := cl.OptValue(kgo.ConsumerGroup).(string)
	ctx, h := work.Start(contextOf(r), log, unitOf(r, group))
	return ctx, h.End
}

// process runs one unit of work through the event path of this adapter: one event, the group
// of the kind, and a recovered panic as an error.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}

// unitOf maps one fetched record onto a unit of work. The record time becomes the start
// time, so the event carries the time the record waited as lag_ms.
func unitOf(r *kgo.Record, group string) work.Unit {
	fields := map[string]any{
		"system":      "kafka",
		"operation":   "process",
		"destination": r.Topic,
		"partition":   r.Partition,
		"offset":      r.Offset,
	}
	if group != "" {
		fields["consumer_group"] = group
	}
	return work.Unit{
		Kind:      work.KindMessage,
		Fields:    fields,
		Carrier:   carrierOf(r.Headers),
		StartedAt: r.Timestamp,
	}
}

// carrierOf wraps the headers of one record as a propagate carrier, so a traceparent header
// joins the trace of the producer.
func carrierOf(headers []kgo.RecordHeader) propagate.Carrier {
	values := make(map[string][]byte, len(headers))
	for _, header := range headers {
		values[header.Key] = header.Value
	}
	return propagate.NewBytesCarrier(values)
}
