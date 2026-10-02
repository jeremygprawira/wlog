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

// Chunks cuts events into runs that hold at most maxItems events and at most maxBytes
// encoded bytes. sizeOf returns one event's encoded size, and a limit of 0 means no limit.
//
// An event that alone passes maxBytes goes to oversize, and no chunk holds it, because no
// request can carry it. The caller drops those events with reason too_large.
func Chunks(events []map[string]any, maxItems, maxBytes int, sizeOf func(map[string]any) int) (chunks [][]map[string]any, oversize []int) {
	var current []map[string]any
	size := 0
	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, current)
			current, size = nil, 0
		}
	}
	for i, event := range events {
		eventSize := sizeOf(event)
		if maxBytes > 0 && eventSize > maxBytes {
			oversize = append(oversize, i)
			continue
		}
		full := maxItems > 0 && len(current) >= maxItems
		over := maxBytes > 0 && size+eventSize > maxBytes
		if len(current) > 0 && (full || over) {
			flush()
		}
		current = append(current, event)
		size += eventSize
	}
	flush()
	return chunks, oversize
}

// SendChunk posts one chunk with post, and while the backend answers 413 with more than
// one event in the chunk it splits the chunk in half and posts each half. A chunk of one
// event that still gets a 413 is refused for good, because no request can carry it.
//
// It returns a *PartialError whose Dropped holds the positions in chunk of the refused
// events, with reason too_large, or nil when every event landed. A failure that is not a
// 413 is returned unchanged, so the pipeline's own retry policy applies.
func SendChunk(ctx context.Context, chunk []map[string]any, post func(context.Context, []map[string]any) error) (*pipeline.PartialError, error) {
	dropped, err := halve(ctx, chunk, 0, post)
	if err != nil {
		return nil, err
	}
	if len(dropped) == 0 {
		return nil, nil
	}
	return &pipeline.PartialError{Dropped: dropped, Reason: "too_large"}, nil
}

// halve posts one run of a chunk. A 413 splits the run in half and posts each half, until
// one event is left, which no request can carry. offset is the position of the run in the
// chunk, so the result names chunk positions.
func halve(ctx context.Context, events []map[string]any, offset int, post func(context.Context, []map[string]any) error) ([]int, error) {
	if len(events) == 0 {
		return nil, nil
	}
	err := post(ctx, events)
	if err == nil {
		return nil, nil
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusRequestEntityTooLarge {
		return nil, err
	}
	if len(events) == 1 {
		return []int{offset}, nil
	}
	middle := len(events) / 2
	left, err := halve(ctx, events[:middle], offset, post)
	if err != nil {
		return nil, err
	}
	right, err := halve(ctx, events[middle:], offset+middle, post)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}
