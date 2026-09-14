package wlog_test

import (
	"context"
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
		ctx, end := wlog.Start(ctx, "op")
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
