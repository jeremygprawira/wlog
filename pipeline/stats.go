// This file holds the counters that tell a caller what the pipeline did.
package pipeline

// Stats is a snapshot of one pipeline's counters.
//
// The numbers are read without a lock held across the work, so they describe the
// moment of the call rather than a frozen state. They are the numbers a health
// endpoint reports.
type Stats struct {
	Sent     int64 // events handed to the sender without an error
	Dropped  int64 // events a full buffer, a refused batch, or a closed pipeline lost
	Retries  int64 // attempts after the first one
	Buffered int   // events waiting in the buffer right now
	Batches  int64 // batches handed to the sender
}
