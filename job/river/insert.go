// This file holds the insert middleware: one call per insert, and the trace metadata of the
// context.
package wlogriver

import (
	"context"
	"encoding/json"

	"github.com/riverqueue/river/rivertype"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// Insert is the River insert middleware that writes the trace headers of the context into
// the metadata of every inserted job, and records one call on the event of the context. Set
// it in Config.JobInsertMiddleware.
type Insert struct{}

// InsertMiddleware returns the insert middleware that writes the trace headers of the
// context into the metadata of every inserted job.
func InsertMiddleware() *Insert { return &Insert{} }

// IsMiddleware marks the type as River middleware. A newer River asks for this method, so
// the same value works in Config.JobInsertMiddleware there.
func (*Insert) IsMiddleware() bool { return true }

// InsertMany writes the trace headers of ctx into the metadata of every job of one batch,
// records one call on the event of ctx, and runs the inner insert. The trace headers reach
// the worker through the job metadata.
func (*Insert) InsertMany(ctx context.Context, many []*rivertype.JobInsertParams, doInner func(context.Context) ([]*rivertype.JobInsertResult, error)) ([]*rivertype.JobInsertResult, error) {
	if len(many) == 0 {
		return doInner(ctx)
	}
	ctx, end := wlog.StartCall(ctx, wlog.Call{
		Kind: "queue", System: "river", Operation: "insert", Target: insertTarget(many),
	})
	for _, params := range many {
		if params != nil {
			params.Metadata = withTraceMetadata(ctx, params.Metadata)
		}
	}
	out, err := doInner(ctx)
	end(resultOf(err))
	return out, err
}

// insertTarget names the kind of a batch of one, and the word batch for several kinds.
func insertTarget(many []*rivertype.JobInsertParams) string {
	if len(many) == 1 && many[0] != nil {
		return many[0].Kind
	}
	return "batch"
}

// withTraceMetadata returns the metadata of one insert with the trace headers of ctx added
// as JSON keys. River stores metadata as a JSON object, so every other key stays. A context
// with no trace context, or metadata that does not parse, keeps the metadata as it was.
func withTraceMetadata(ctx context.Context, metadata []byte) []byte {
	if _, ok := propagate.FromContext(ctx); !ok {
		return metadata
	}
	carrier := propagate.MapCarrier{}
	propagate.Inject(ctx, carrier)
	fields := map[string]any{}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &fields); err != nil {
			return metadata
		}
	}
	for key, value := range carrier {
		fields[key] = value
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return metadata
	}
	return out
}

// resultOf builds the call result of one finished insert.
func resultOf(err error) wlog.CallResult {
	if err == nil {
		return wlog.CallResult{Status: "ok"}
	}
	return wlog.CallResult{Err: err}
}
