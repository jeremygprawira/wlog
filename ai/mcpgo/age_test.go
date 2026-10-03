package wlogmcpgo

import (
	"context"
	"testing"
	"time"
)

// TestMcpgo_A3_TakeReturnsExpiredEntry proves a request that outlives the pending
// TTL still finishes. Expiry belongs to the prune loop, not to take.
func TestMcpgo_A3_TakeReturnsExpiredEntry(t *testing.T) {
	pend := newPending()
	key := pendingKey{session: "s", id: "req-1"}
	ctx := context.WithValue(context.Background(), struct{ name string }{name: "mark"}, "kept")
	pend.store(nil, key, ctx, ctx, nil)

	pend.mu.Lock()
	entry := pend.entries[key]
	entry.expires = time.Now().Add(-time.Second)
	pend.entries[key] = entry
	pend.mu.Unlock()

	got, _, ok := pend.take(key)
	if !ok {
		t.Fatal("take dropped an expired entry, so a long request gives no event")
	}
	if got.Value(struct{ name string }{name: "mark"}) != "kept" {
		t.Fatalf("take returned the wrong context: %v", got)
	}
}
