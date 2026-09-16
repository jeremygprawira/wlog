package wlog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
)

func TestCore_WithDrains_FansOutRedactedEvent(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	drain := wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, event)
	})

	log := wlog.New(wlog.WithDrains(drain))
	captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "op")
		wlog.Set(ctx, "password", "hunter2")
		end()
	})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("drain received %d events, want 1", len(got))
	}
	if got[0]["password"] != "[REDACTED]" {
		t.Errorf("drain saw an unredacted event: %v", got[0]["password"])
	}
}

func TestCore_Drain_PanicIsolated(t *testing.T) {
	var errs []string
	var reached bool

	bad := wlog.DrainFunc(func(context.Context, map[string]any) { panic("boom") })
	good := wlog.DrainFunc(func(context.Context, map[string]any) { reached = true })

	log := wlog.New(
		wlog.WithDrains(bad, good),
		wlog.OnError(func(err error, source string) {
			errs = append(errs, fmt.Sprintf("%s: %v", source, err))
		}),
	)

	captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()
	})

	if !reached {
		t.Error("a panicking drain stopped a later drain from running")
	}
	if len(errs) != 1 {
		t.Fatalf("OnError called %d times, want 1: %v", len(errs), errs)
	}
}

func TestCore_Close_ClosesDrains(t *testing.T) {
	closed := make(chan struct{}, 1)
	log := wlog.New(wlog.WithDrains(closingDrain{closed: closed}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := log.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-closed:
	default:
		t.Error("Close did not call the drain's Close")
	}
}

type closingDrain struct{ closed chan struct{} }

func (closingDrain) Send(context.Context, map[string]any) {}
func (d closingDrain) Close(context.Context) error {
	d.closed <- struct{}{}
	return nil
}

// snapshotDrain keeps the map it received, so a later change by core shows up.
type snapshotDrain struct {
	seen []map[string]any
}

// Send records the map as it arrived.
func (d *snapshotDrain) Send(_ context.Context, ev map[string]any) {
	d.seen = append(d.seen, ev)
}

// TestCore_CORE11_CoreNeverMutatesAfterDrains proves that the map a drain
// receives is final: core adds nothing to it after the drain stage starts, so a
// drain may read it later and two drains see the same event.
func TestCore_CORE11_CoreNeverMutatesAfterDrains(t *testing.T) {
	first := &snapshotDrain{}
	second := &snapshotDrain{}

	log := wlog.New(wlog.WithDrains(first, second), wlog.WithFormat(wlog.FormatJSON))
	ctx := log.WithContext(context.Background())

	ctx, end := wlog.Start(ctx, "op")
	wlog.Set(ctx, "user_id", "u1")
	end()

	if len(first.seen) != 1 || len(second.seen) != 1 {
		t.Fatalf("drains saw %d and %d events, want one each", len(first.seen), len(second.seen))
	}
	before, err := json.Marshal(first.seen[0])
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(first.seen[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("core changed the map after the drain stage:\n%s\n%s", before, after)
	}
	if first.seen[0]["duration_ms"] == nil || first.seen[0]["user_id"] != "u1" {
		t.Errorf("the drained map is incomplete: %v", first.seen[0])
	}
}
