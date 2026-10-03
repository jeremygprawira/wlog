package wloglangchaingo

import (
	"context"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLangchaingo_A6_DropsUnendedToolStarts proves a tool start with no end does not
// grow the handler without bound. A start past the cap, and a start older than the
// TTL, is reported and not recorded.
func TestLangchaingo_A6_DropsUnendedToolStarts(t *testing.T) {
	var codes []string
	log, rec := wlogtest.New(t, wlog.OnProblem(func(p wlog.Problem) {
		codes = append(codes, p.Code)
	}))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	h := Handler(log).(*handler)
	first := context.WithValue(ctx, "first", "first")
	h.HandleToolStart(first, "in")
	for i := 1; i < callCap; i++ {
		h.HandleToolStart(context.WithValue(ctx, i, i), "in")
	}
	extra := context.WithValue(ctx, "extra", "extra")
	h.HandleToolStart(extra, "in")
	if len(h.tools[extra]) != 0 {
		t.Fatal("the start past the cap was stored")
	}

	h.mu.Lock()
	stack := h.tools[first]
	stack[0].at = time.Now().Add(-callTTL - time.Second)
	h.tools[first] = stack
	h.mu.Unlock()
	fresh := context.WithValue(ctx, "fresh", "fresh")
	h.HandleToolStart(fresh, "in")
	if len(h.tools[first]) != 0 {
		t.Fatal("an aged start was kept")
	}
	h.HandleToolEnd(fresh, "out")
	end()

	reports := 0
	for _, code := range codes {
		if code == "WLOG_CAP_REACHED" {
			reports++
		}
	}
	if reports < 2 {
		t.Fatalf("cap reports = %d, want the cap drop and the aged drop", reports)
	}
	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want only the fresh start", len(calls))
	}
}
