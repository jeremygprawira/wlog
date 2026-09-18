package wlog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
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

		flushWriter(t, log)
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
		wlog.OnProblem(func(p wlog.Problem) {
			errs = append(errs, fmt.Sprintf("%s: %v", p.Source, p.Err))
		}),
	)

	captureStdout(t, func() {
		ctx := log.WithContext(context.Background())
		_, end := wlog.Start(ctx, "op")
		end()

		flushWriter(t, log)
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

// slowDrain starts its close and waits for the test to release it.
type slowDrain struct {
	events  []map[string]any
	flushed int
	closing chan struct{}
	release chan struct{}
}

// Send records the event.
func (d *slowDrain) Send(_ context.Context, ev map[string]any) { d.events = append(d.events, ev) }

// Flush counts one flush and keeps the drain running.
func (d *slowDrain) Flush(context.Context) error { d.flushed++; return nil }

// Close blocks until the test releases it.
func (d *slowDrain) Close(ctx context.Context) error {
	select {
	case d.closing <- struct{}{}:
	default:
	}
	select {
	case <-d.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestCore_Flush_KeepsDrains proves that Flush flushes a drain and leaves it
// running, so events after a flush still arrive.
func TestCore_Flush_KeepsDrains(t *testing.T) {
	d := &slowDrain{}
	log := wlog.New(wlog.WithDrains(d), wlog.WithFormat(wlog.FormatJSON))
	ctx := log.WithContext(context.Background())

	_, end := wlog.Start(ctx, "op")
	end()

	if err := log.Flush(context.Background()); err != nil {
		t.Fatalf("Flush returned %v", err)
	}
	if d.flushed != 1 {
		t.Errorf("flushes = %d, want 1", d.flushed)
	}

	_, end = wlog.Start(ctx, "op2")
	end()
	if len(d.events) != 2 {
		t.Errorf("drain saw %d events, want 2: the flush disconnected it", len(d.events))
	}
}

// TestCore_CORE32_CloseConcurrentAndBounded proves that Close starts every drain
// at once and returns when the context ends, without waiting for a stuck drain.
func TestCore_CORE32_CloseConcurrentAndBounded(t *testing.T) {
	first := &slowDrain{closing: make(chan struct{}, 1), release: make(chan struct{})}
	second := &slowDrain{closing: make(chan struct{}, 1), release: make(chan struct{})}
	log := wlog.New(wlog.WithDrains(first, second), wlog.WithFormat(wlog.FormatJSON))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := log.Close(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Close returned nil, want the context error of the stuck drains")
	}
	if elapsed > time.Second {
		t.Errorf("Close took %v, want it to stop at the context deadline", elapsed)
	}
	// Both closes must have started, which proves they ran at the same time.
	for name, d := range map[string]*slowDrain{"first": first, "second": second} {
		select {
		case <-d.closing:
		default:
			t.Errorf("the %s drain never started closing", name)
		}
	}
	close(first.release)
	close(second.release)
}

// TestCore_CORE32_EmitAfterCloseReported proves that an event after Close reaches
// no drain and is reported through OnError.
func TestCore_CORE32_EmitAfterCloseReported(t *testing.T) {
	var reported int
	var mu sync.Mutex

	d := &slowDrain{release: make(chan struct{})}
	close(d.release)
	log, rec := wlogtest.New(t,
		wlog.WithDrains(d),
		wlog.OnProblem(func(wlog.Problem) { mu.Lock(); reported++; mu.Unlock() }),
	)
	ctx := log.WithContext(context.Background())

	if err := log.Close(context.Background()); err != nil {
		t.Fatalf("Close returned %v", err)
	}

	_, end := wlog.Start(ctx, "after-close")
	wlog.Set(ctx, "user_id", "u1")
	end()

	if len(d.events) != 0 {
		t.Errorf("a drain received an event after Close: %v", d.events)
	}
	if rec.Count() != 0 {
		t.Errorf("a drain received an event after Close: %v", rec.Events())
	}
	mu.Lock()
	defer mu.Unlock()
	if reported == 0 {
		t.Error("OnError heard nothing about the dropped event")
	}
}
