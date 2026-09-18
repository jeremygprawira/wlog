// This file tests the problem layer from outside the package: the catalog matches the
// document, and a late write reaches a caller's handler.
package wlog_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// TestProblems_BET4_EveryCodeDocumented proves that docs/problems.md holds a section
// for every code the catalog reports. A code with no section leaves a reader with a
// line they cannot look up.
func TestProblems_BET4_EveryCodeDocumented(t *testing.T) {
	body, err := os.ReadFile("docs/problems.md")
	if err != nil {
		t.Fatal(err)
	}
	document := string(body)
	for _, p := range wlog.Problems() {
		if !strings.Contains(document, "## "+p.Code) {
			t.Errorf("docs/problems.md has no section for %s", p.Code)
		}
	}
}

// TestProblems_PAR3_LateWriteReported proves that a write on an event that already
// emitted, with no open parent, reaches the caller instead of vanishing.
func TestProblems_PAR3_LateWriteReported(t *testing.T) {
	got := make(chan wlog.Problem, 4)
	// A drain keeps the WLOG_SILENT_NO_DRAIN report out of the channel, so this test
	// reads the report it asks about.
	sink := wlog.DrainFunc(func(context.Context, map[string]any) {})
	log := wlog.New(wlog.WithSilent(), wlog.WithDrains(sink), wlog.OnProblem(func(p wlog.Problem) { got <- p }))

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "integration.op")
	end()
	wlog.Set(ctx, "late", "value")

	select {
	case p := <-got:
		if p.Code != "WLOG_LATE_WRITE" {
			t.Fatalf("code = %q, want WLOG_LATE_WRITE", p.Code)
		}
		if p.Fix == "" || p.Link == "" {
			t.Errorf("the catalog did not fill the report: %+v", p)
		}
	default:
		t.Fatal("a write after the end func reported nothing")
	}
}
