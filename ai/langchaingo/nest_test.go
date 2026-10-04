package wloglangchaingo

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLangchaingo_A7_NestedStartsStayDistinct proves nested starts on one context
// each become a call. One slot used to keep only the latest start.
func TestLangchaingo_A7_NestedStartsStayDistinct(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	h := Handler(log)
	for i := 0; i < 4; i++ {
		h.HandleToolStart(ctx, "in")
	}
	for i := 0; i < 4; i++ {
		h.HandleToolEnd(ctx, "out")
	}
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 4 {
		t.Fatalf("calls = %d, want 4 nested tool calls", len(calls))
	}
}
