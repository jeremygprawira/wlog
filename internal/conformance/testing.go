// This file holds the test plumbing every conformance suite shares: the small part of
// testing.T a suite uses, the adapter that runs a suite under a real test, and the
// capture that records failures so a broken adapter can be checked.
package conformance

import (
	"fmt"
	"strings"
	"testing"
)

// TB is the part of testing.T that a suite uses, so a suite can run under a real test and
// under a capture that collects failures.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Run(name string, fn func(TB)) bool
}

// Tester adapts *testing.T to TB, so Run reports through the real test.
type Tester struct{ *testing.T }

// Run runs one scenario with an adapted tester.
func (t Tester) Run(name string, fn func(TB)) bool {
	return t.T.Run(name, func(sub *testing.T) { fn(Tester{sub}) })
}

// Capture records the failures of one suite run, so a test can prove that a broken
// adapter fails and read the report.
type Capture struct {
	Failures []string
}

// Helper does nothing, because a capture holds no test state.
func (c *Capture) Helper() {}

// Errorf records one failure.
func (c *Capture) Errorf(format string, args ...any) {
	c.Failures = append(c.Failures, fmt.Sprintf(format, args...))
}

// Fatalf records one failure. It does not stop the run, because a suite reports with
// Errorf and returns.
func (c *Capture) Fatalf(format string, args ...any) {
	c.Failures = append(c.Failures, fmt.Sprintf(format, args...))
}

// Run runs one scenario against the same capture.
func (c *Capture) Run(_ string, fn func(TB)) bool {
	fn(c)
	return true
}

// Reports reports whether one scenario name appears in the recorded failures.
func (c *Capture) Reports(name string) bool {
	for _, failure := range c.Failures {
		if strings.Contains(failure, name) {
			return true
		}
	}
	return false
}
