package wlogmcpgo

import (
	"context"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestMcpgo_A15_CapDropIsReported proves the start past the cap is reported.
func TestMcpgo_A15_CapDropIsReported(t *testing.T) {
	var codes []string
	log, _ := wlogtest.New(t, wlog.OnProblem(func(p wlog.Problem) {
		codes = append(codes, p.Code)
	}))
	pend := newPending()
	ctx := log.WithContext(context.Background())
	for i := 0; i < pendingCapacity; i++ {
		pend.store(log, pendingKey{id: string(rune(i))}, ctx, ctx, nil)
	}
	pend.store(log, pendingKey{id: "over"}, ctx, ctx, nil)
	saw := false
	for _, code := range codes {
		if code == "WLOG_CAP_REACHED" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("a start past the cap was not reported")
	}
}

// TestMcpgo_A15_PruneEndsWithError proves an expired entry is ended with an error
// and reported when the next start prunes it.
func TestMcpgo_A15_PruneEndsWithError(t *testing.T) {
	var codes []string
	log, rec := wlogtest.New(t, wlog.OnProblem(func(p wlog.Problem) {
		codes = append(codes, p.Code)
	}))
	pend := newPending()
	ctx := log.WithContext(context.Background())
	eventCtx, h := work.Start(ctx, log, work.Unit{Kind: work.KindRPC})
	key := pendingKey{session: "s", id: "old"}
	pend.store(log, key, ctx, eventCtx, h)
	pend.mu.Lock()
	entry := pend.entries[key]
	entry.expires = time.Now().Add(-time.Second)
	pend.entries[key] = entry
	pend.mu.Unlock()
	pend.store(log, pendingKey{session: "s", id: "new"}, ctx, ctx, nil)

	if rec.Count() != 1 {
		t.Fatalf("events = %d, want the pruned event", rec.Count())
	}
	if rec.Last()["error"] == nil {
		t.Fatal("pruned event has no error")
	}
	saw := false
	for _, code := range codes {
		if code == "WLOG_CAP_REACHED" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("a pruned start was not reported")
	}
}
