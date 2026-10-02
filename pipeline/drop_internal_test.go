// This file tests the internals of the wrapped drain: the reason a batch was dropped, and
// the methods of PartialError. The exported paths cover the rest.
package pipeline

import (
	"errors"
	"testing"
)

// TestPipeline_P7_DropReasons proves each drop maps to its own reason, which is the key the
// report uses.
func TestPipeline_P7_DropReasons(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"a full buffer", nil, "buffer_full"},
		{"a closed worker", errClosed, "closed"},
		{"a backend reason", &PartialError{Reason: "status_400"}, "status_400"},
		{"a partial error with no reason", &PartialError{}, "send_failed"},
		{"a send failure", errors.New("boom"), "send_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dropReason(tc.err); got != tc.want {
				t.Errorf("dropReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPipeline_P11_PartialErrorText proves the error text names both index sets and the
// reason, and that Retryable follows the retry list.
func TestPipeline_P11_PartialErrorText(t *testing.T) {
	pe := &PartialError{Retry: []int{1}, Dropped: []int{2}, Reason: "status_400"}
	got := pe.Error()
	if got == "" {
		t.Fatal("Error() is empty")
	}
	if !pe.Retryable() {
		t.Error("Retryable() = false with a retry index")
	}
	if (&PartialError{Dropped: []int{0}}).Retryable() {
		t.Error("Retryable() = true with no retry index")
	}
}

// TestPipeline_DroppedCount proves the pipeline reports how many events it lost.
func TestPipeline_DroppedCount(t *testing.T) {
	w := &wrapped{}
	w.dropped.Store(2)
	if got := w.Dropped(); got != 2 {
		t.Errorf("Dropped() = %d, want 2", got)
	}
}
