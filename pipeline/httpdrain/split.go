// This file holds the batch split the HTTP drains share. One helper cuts a batch at a
// backend's item and byte limits, and one posts a chunk while halving it after a 413, so
// one request that is too large does not lose the whole batch.
package httpdrain

import (
	"context"
	"errors"
	"net/http"

	"github.com/jeremygprawira/wlog/pipeline"
)

// Chunk is one run of a batch: the half-open index range [Start, End) into the events the
// caller passed to Chunks.
type Chunk struct {
	Start int
	End   int
}

// Chunks cuts events into runs that hold at most maxItems events and at most maxBytes
// encoded bytes. sizeOf returns one event's encoded size, and a limit of 0 means no limit.
//
// It returns the chunks in order, and the indexes of events that alone pass maxBytes,
// which no request can carry. The caller drops those events with reason too_large.
func Chunks(events []map[string]any, maxItems, maxBytes int, sizeOf func(map[string]any) int) (chunks []Chunk, oversize []int) {
	start := 0
	size := 0
	for i, event := range events {
		eventSize := sizeOf(event)
		if maxBytes > 0 && eventSize > maxBytes {
			// The event is too large on its own, so the run before it ends here and the
			// run after it starts at the next event.
			if i > start {
				chunks = append(chunks, Chunk{Start: start, End: i})
			}
			oversize = append(oversize, i)
			start, size = i+1, 0
			continue
		}
		if i > start {
			full := maxItems > 0 && i-start >= maxItems
			over := maxBytes > 0 && size+eventSize > maxBytes
			if full || over {
				chunks = append(chunks, Chunk{Start: start, End: i})
				start, size = i, 0
			}
		}
		size += eventSize
	}
	if len(events) > start {
		chunks = append(chunks, Chunk{Start: start, End: len(events)})
	}
	return chunks, oversize
}

// SendChunk posts the chunk of events with post, and while the backend answers 413 with
// more than one event in the chunk it splits the chunk in half and posts each half. A
// chunk of one event that still gets a 413 is refused for good, because no request can
// carry it.
//
// It returns a *PartialError whose Dropped holds the batch indexes of the refused events,
// with reason too_large, or nil when every event landed. A failure that is not a 413 is
// returned unchanged, so the pipeline's own retry policy applies.
func SendChunk(ctx context.Context, events []map[string]any, chunk Chunk, post func(context.Context, []map[string]any) error) (*pipeline.PartialError, error) {
	dropped, err := halve(ctx, events, chunk.Start, chunk.End, post)
	if err != nil {
		return nil, err
	}
	if len(dropped) == 0 {
		return nil, nil
	}
	return &pipeline.PartialError{Dropped: dropped, Reason: "too_large"}, nil
}

// shiftPartial returns a copy of pe whose indexes are offset by n, so a per-item result
// names positions in the whole batch.
func shiftPartial(pe *pipeline.PartialError, n int) *pipeline.PartialError {
	if n == 0 {
		return pe
	}
	shifted := &pipeline.PartialError{Reason: pe.Reason}
	for _, i := range pe.Retry {
		shifted.Retry = append(shifted.Retry, i+n)
	}
	for _, i := range pe.Dropped {
		shifted.Dropped = append(shifted.Dropped, i+n)
	}
	return shifted
}

// halve posts events[start:end]. A 413 splits the run in half and posts each half, until
// one event is left, which no request can carry.
func halve(ctx context.Context, events []map[string]any, start, end int, post func(context.Context, []map[string]any) error) ([]int, error) {
	if end <= start {
		return nil, nil
	}
	err := post(ctx, events[start:end])
	if err == nil {
		return nil, nil
	}
	// A per-item result names positions in the slice post received, so it is shifted to
	// the batch the caller passed.
	var itemErr *pipeline.PartialError
	if errors.As(err, &itemErr) {
		return nil, shiftPartial(itemErr, start)
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusRequestEntityTooLarge {
		return nil, err
	}
	if end-start == 1 {
		return []int{start}, nil
	}
	middle := start + (end-start)/2
	left, err := halve(ctx, events, start, middle, post)
	if err != nil {
		return nil, err
	}
	right, err := halve(ctx, events, middle, end, post)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}
