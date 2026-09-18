// This file tests the reporter from inside the package, where a test can set the rate
// limit window and read the fold count of a code.
package wlog

import (
	"bytes"
	"strings"
	"testing"
)

// TestProblems_CORE17_LateWriteCounted proves that the reporter counts the reports it
// folds, so a caller learns how often a fault happened. The window is 0 here, so
// every report reaches the handler.
func TestProblems_CORE17_LateWriteCounted(t *testing.T) {
	var got []Problem
	r := newProblemReporter()
	r.setHandler(func(p Problem) { got = append(got, p) })

	r.deliver(Problem{Code: codeLateWrite, Source: "write"})
	r.deliver(Problem{Code: codeLateWrite, Source: "write"})

	if len(got) != 2 {
		t.Fatalf("handler calls = %d, want 2", len(got))
	}
	if got[1].Count != 2 {
		t.Errorf("second report Count = %d, want 2", got[1].Count)
	}
}

// TestProblems_DefaultRateLimited proves that the default setup writes one line per
// code per minute, and that a repeated report is folded rather than written again.
func TestProblems_DefaultRateLimited(t *testing.T) {
	var buf bytes.Buffer
	r := newProblemReporter()
	r.setHandler(defaultProblemHandler(&buf))

	r.deliver(Problem{Code: codeLateWrite, Source: "write"})
	r.deliver(Problem{Code: codeLateWrite, Source: "write"})
	r.deliver(Problem{Code: codeNoEvent, Source: "Set"})

	if lines := strings.Count(buf.String(), "\n"); lines != 2 {
		t.Errorf("lines = %d, want 2 (one per code):\n%s", lines, buf.String())
	}
	if !strings.Contains(buf.String(), `"code":"WLOG_LATE_WRITE"`) {
		t.Errorf("the default handler did not write the code:\n%s", buf.String())
	}
}

// TestProblems_CatalogIsComplete proves that every catalog entry carries the text a
// reader needs, and that no two entries share a code.
func TestProblems_CatalogIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Problems() {
		if p.Code == "" || p.Why == "" || p.Fix == "" || p.Link == "" {
			t.Errorf("catalog entry is incomplete: %+v", p)
		}
		if !strings.HasSuffix(p.Link, strings.ToLower(p.Code)) {
			t.Errorf("link %q does not anchor to %s", p.Link, p.Code)
		}
		if seen[p.Code] {
			t.Errorf("code %s appears twice", p.Code)
		}
		seen[p.Code] = true
	}
}

// TestProblems_ReportBeforeHandlerWaits proves that a report which arrives before the
// caller sets a handler waits in the queue, and then reaches that handler.
func TestProblems_ReportBeforeHandlerWaits(t *testing.T) {
	var got []Problem
	r := newProblemReporter()
	r.deliver(Problem{Code: codeLateWrite, Source: "write"})
	if len(r.pending) != 1 {
		t.Fatalf("pending reports = %d, want 1", len(r.pending))
	}
	r.setHandler(func(p Problem) { got = append(got, p) })
	if len(got) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(got))
	}
	if len(r.pending) != 0 {
		t.Errorf("the queue kept %d reports after the handler ran", len(r.pending))
	}
}
