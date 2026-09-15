package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/memory"
)

// recomputeHash redoes the exact sha256(prev||canonical) recipe chain.send used, over
// the event as it stood right before Chain assigned audit.hash/audit.prev_hash.
func recomputeHash(t *testing.T, event map[string]any) string {
	t.Helper()
	prev, _ := event["audit.prev_hash"].(string)

	clean := make(map[string]any, len(event))
	for k, v := range event {
		if k == "audit.hash" || k == "audit.prev_hash" {
			continue
		}
		clean[k] = v
	}
	canon, err := canonicalJSON(clean)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	sum := sha256.Sum256(append([]byte(prev), canon...))
	return hex.EncodeToString(sum[:])
}

// verifyChain follows prev_hash links from the root ("") through every event (order in
// the slice does not matter), recomputing each hash, the same check audit.Verify will
// later run over a journal file.
func verifyChain(t *testing.T, events []map[string]any) {
	t.Helper()

	byPrev := make(map[string]map[string]any, len(events))
	for _, e := range events {
		prev, _ := e["audit.prev_hash"].(string)
		if _, dup := byPrev[prev]; dup {
			t.Fatalf("two events share prev_hash %q; not a single chain", prev)
		}
		byPrev[prev] = e
	}

	prev := ""
	for i := 0; i < len(events); i++ {
		e, ok := byPrev[prev]
		if !ok {
			t.Fatalf("chain broken after %d of %d events: no event with prev_hash %q", i, len(events), prev)
		}
		wantHash, _ := e["audit.hash"].(string)
		if got := recomputeHash(t, e); got != wantHash {
			t.Fatalf("event %d: stored hash %q, recomputed %q", i, wantHash, got)
		}
		prev = wantHash
	}
}

func testAuditRecord() Record {
	return Record{
		Actor:   Actor{Type: "user", ID: "u1", Email: "a@example.com"},
		Action:  "invoice.refund",
		Target:  Target{Type: "invoice", ID: "inv1"},
		Outcome: "success",
	}
}

func TestAudit_Chain_LinksHashesInOrder(t *testing.T) {
	mem := memory.New(0)
	log := wlog.New(wlog.WithDrains(Chain(), mem))
	ctx := log.WithContext(context.Background())

	for i := 0; i < 5; i++ {
		Do(ctx, testAuditRecord())
	}

	events := mem.Snapshot()
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}
	for i, e := range events {
		if _, ok := e["audit.hash"].(string); !ok {
			t.Fatalf("event %d missing audit.hash", i)
		}
	}
	verifyChain(t, events)
}

func TestAudit_Chain_ConcurrentEmitsFormOneChain(t *testing.T) {
	mem := memory.New(0)
	log := wlog.New(wlog.WithDrains(Chain(), mem))
	ctx := log.WithContext(context.Background())

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Do(ctx, testAuditRecord())
		}()
	}
	wg.Wait()

	events := mem.Snapshot()
	if len(events) != n {
		t.Fatalf("got %d events, want %d", len(events), n)
	}
	verifyChain(t, events)
}

func TestAudit_Chain_IgnoresNonAuditEvents(t *testing.T) {
	mem := memory.New(0)
	log := wlog.New(wlog.WithDrains(Chain(), mem))
	ctx := log.WithContext(context.Background())

	_, end := wlog.Start(ctx, "plain.op")
	wlog.Set(ctx, "x", 1)
	end()

	events := mem.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if _, ok := events[0]["audit.hash"]; ok {
		t.Errorf("non-audit event got a hash: %v", events[0])
	}
}
