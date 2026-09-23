// This file holds the error a Sender returns when a backend accepts part of a batch.
// The error names the batch indexes to send again and the indexes the backend refused
// for good, so the worker retries only the events that need it.
package pipeline

import (
	"fmt"
	"strings"
	"time"
)

// PartialError is what a Sender returns when a backend accepts some events of a batch
// and refuses the rest. An index is a position in the batch that the Sender received.
//
// Retry holds the indexes to send again. Dropped holds the indexes the backend refused
// for good. Reason names the backend error type or code, and it never holds an event
// value. An index outside the batch is ignored, and an index in both lists counts as
// dropped.
type PartialError struct {
	Retry   []int  // batch indexes to send again
	Dropped []int  // batch indexes that the backend refused for good
	Reason  string // the backend's error type or code, never an event value
}

// Error describes the two index sets and the reason.
func (e *PartialError) Error() string {
	var b strings.Builder
	b.WriteString("pipeline: partial batch")
	fmt.Fprintf(&b, " (retry %d, dropped %d)", len(e.Retry), len(e.Dropped))
	if e.Reason != "" {
		b.WriteString(": ")
		b.WriteString(e.Reason)
	}
	return b.String()
}

// Retryable reports whether any event needs another try. An empty Retry list is final.
func (e *PartialError) Retryable() bool { return len(e.Retry) > 0 }

// RetryAfter is zero, because a partial batch carries no server wait. The worker uses
// its normal backoff.
func (e *PartialError) RetryAfter() time.Duration { return 0 }
